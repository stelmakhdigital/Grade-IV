// Package runner — выполнение кода кандидата (WP-6, ADR-003).
//
// Режимы:
//   - "subprocess" (dev): честный интерпретатор хоста, изолированный cwd,
//     минимальный env, таймаут, ограничение вывода. Только для разработки.
//   - "docker" (prod): docker run --rm с лимитами ADR-003
//     (--network=none, --cpus=1, --memory=512m, --pids-limit=128, --read-only,
//     --tmpfs, --user 1000:1000).
//
// Отклонение от ADR-003 (зафиксировано в docs/DECISIONS.md): WP-6 — контейнер
// на каждый run (упрощение MVP); long-lived контейнер на сессию — бэклог Operations.
package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Пороки по умолчанию (ADR-003: результат ≤ 10 с).
const (
	DefaultTimeout  = 10 * time.Second
	DefaultMaxBytes = 1 << 20 // 1 МБ на stdout/stderr
)

// go платит за сборку std: GOCACHE — per-run (см. cleanEnv и ENV docker-режима),
// поэтому 10 с из ADR-003 уходят на компиляцию и самим тестам бюджета не
// оставляет (BUG-20261122-035). Отдельный бюджет вместо явного таймаута в
// запросе: протокол /runs таймаута не передаёт.
const defaultTimeoutGo = 60 * time.Second

// RLIMIT по умолчанию для subprocess-режима (аналог лимитов docker-режима
// ADR-003: --memory=512m, --cpus=1; dev-режим, где docker недоступен):
// CPU — те же 10 с (RLIMIT_CPU), память — RLIMIT_AS. Важное отличие: RLIMIT_AS
// ограничивает ВИРТУАЛЬНОЕ адресное пространство (VA), а не RSS (в отличие от
// cgroup --memory). Go-рантайм/тулчейн резервируют VA (arena/компиляция) много
// больше фактического RSS: холодная сборка std (GOCACHE per-run, как и раньше)
// требует >1.5 ГБ VA и работает при 2 ГБ (измерено) — для go берём 2048 МБ;
// для python VA≈RSS — 512 МБ, как у docker. Точный RSS-потолок в rlimit на
// Linux не выразить (RLIMIT_RSS не поддерживается) — known limitation
// (ADR-003 поправка): защита subprocess-режима — от «виснет/OOM-хост»,
// не точный RSS-бюджет (это — docker-режим).
const (
	defaultMemLimitGo     = 2048 << 20 // VA-потолок для go-запусков (см. выше)
	defaultMemLimitPython = 512 << 20  // как docker --memory=512m
	defaultMemLimitShell  = 256 << 20  // spec-19: shell-задачам хватает (сортировки/парсинг)
	defaultCPUSecs        = 10         // как docker-режим: результат ≤ 10 с (ADR-003)
)

// Ошибки (маппинг на HTTP — в handlers).
var (
	ErrUnsupportedStack = errors.New("неподдерживаемый стек")
	ErrNoDocker         = errors.New("docker недоступен")
	ErrBadFiles         = errors.New("недопустимые файлы")
)

// TestResult — результат отдельного теста.
type TestResult struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
}

// Result — результат запуска (контракт ARCHITECTURE.md §4.4).
type Result struct {
	ExitCode   int          `json:"exit_code"`
	Stdout     string       `json:"stdout"`
	Stderr     string       `json:"stderr"`
	DurationMS int          `json:"duration_ms"`
	Passed     bool         `json:"passed"`
	Timeout    bool         `json:"timeout"`
	Tests      []TestResult `json:"tests"`
}

// Config — параметры раннера.
type Config struct {
	Mode     string // "subprocess" | "docker"
	Timeout  time.Duration
	MaxBytes int
	// Images: stack → docker-образ (по умолчанию golang:1.24 / python:3.12-slim).
	Images map[string]string
	// Commands: stack → команда тестов (по умолчанию — см. New).
	// Тестируется подменой на произвольные команды.
	// В команде %GRADER% раскрывается в каталог доверенных файлов (см. Run):
	// в docker — отдельный ro-mount /grade, в subprocess — соседний каталог.
	Commands map[string]string
	// WorkDirBase — корень рабочих каталогов (по умолчанию $SANDBOX_WORKDIR_BASE
	// или ~/.local/share/grade-sandbox; НЕ /tmp — см. newWorkDir).
	WorkDirBase string
	// Subprocess-лимиты (dev-режим, аналог docker ADR-003; 0 — значения по
	// умолчанию выше): RLIMIT_AS (байты) и RLIMIT_CPU (сек).
	MemLimitBytes int
	CPUSecs       int
	// MaxConcurrent — предел одновременных запусков (spec-14/C3: N параллельных
	// холодных go test ≈ N×2 ГБ VA → OOM хоста). 0 → defaultMaxConcurrent.
	MaxConcurrent int
	// GoCache — общий кэш сборки go между прогонами (SANDBOX_GO_CACHE).
	// В docker-режиме — ИМЯ docker-тома (монтируется в /cache, kэш в
	// /cache/go-build), в subprocess — каталог на хосте. Пусто — кэш per-run
	// (/work/.gocache), то есть каждая сборка пересобирает std заново.
	// Зачем: cold std build стоит ~40 с (см. defaultTimeoutGo), а gate генерации
	// делает 1 эталон + до 6 мутантов = до 7 сборок — 4,5 минуты на задачу.
	GoCache string
}

// defaultMaxConcurrent — см. Config.MaxConcurrent.
const defaultMaxConcurrent = 4

// Runner — выполнение запусков.
type Runner struct {
	cfg Config
	sem chan struct{} // spec-14/C3: semaphore одновременных запусков
}

// New создаёт раннер (режим и пороки — из конфига).
func New(cfg Config) *Runner {
	// Timeout == 0 — не задан, берётся по стеку в Run (go — дольше, см. defaultTimeoutGo).
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = DefaultMaxBytes
	}
	// spec-20: частичные оверрайды (SANDBOX_IMAGE_*) мерджатся на дефолты —
	// переопределение одного стека (python с pytest) не сбрасывает остальные.
	defaults := map[string]string{
		"go": "golang:1.24", "python": "python:3.12-slim",
		// spec-19: shell — debian-slim (bash + coreutils: grep/sort/uniq/head;
		// без awk/procps — задачи не зависят от них).
		"shell": "debian:12-slim",
	}
	for stack, img := range cfg.Images {
		if img != "" {
			defaults[stack] = img
		}
	}
	cfg.Images = defaults
	// Команды мерджатся как образы: переопределение одного стека не сбрасывает
	// остальные (dev случай: SANDBOX_CMD_PYTHON указывает python с pytest, go и
	// shell остаются на дефолтах).
	// Градатор берётся из %GRADER% (отдельный от кандидата каталог —
	// BUG-20261122-033), а не из рабочего каталога решения.
	cmds := map[string]string{
		// go: тесты банка подмешиваются в пакет оверлеем (go test видит их
		// как файлы пакета, но на диске кандидата их нет).
		"go": "go test -count=1 -json -overlay %GRADER%/overlay.json ./...",
		// python: тесты банка лежат в каталоге градатора (test_*.py).
		"python": "python3 -m pytest -q -p no:cacheprovider %GRADER%",
		// spec-19: shell-задания — чек-скрипт банка (check.sh) запускает
		// решение кандидата (solution.sh) и ассертит результат.
		"shell": "bash %GRADER%/check.sh",
	}
	for stack, cmd := range cfg.Commands {
		if cmd != "" {
			cmds[stack] = cmd
		}
	}
	cfg.Commands = cmds
	maxConc := cfg.MaxConcurrent
	if maxConc <= 0 {
		maxConc = defaultMaxConcurrent
	}
	return &Runner{cfg: cfg, sem: make(chan struct{}, maxConc)}
}

// Mode — активный режим.
func (r *Runner) Mode() string { return r.cfg.Mode }

// Run выполняет тесты (action "test").
//
//	files   — файлы кандидата: пишутся в run-каталог, он же cwd прогона;
//	trusted — доверенные файлы банка (градатор: check.sh, *_test.go, test_*.py):
//	          пишутся в СОСЕДНИЙ каталог, который в docker монтируется в /grade
//	          только на чтение.
//
// Разведение каталогов — BUG-20261122-033: раньше файлы банка лежали в cwd
// решения, и одна сдача `cat check.sh` возвращала градатор в stdout прогона и
// одновременно давала гарантированный pass (ожидаемые значения читались из того
// же файла); `sed -i check.sh` переписывал чек — workdir примонтирован :rw.
//
// Что закрывается, а что нет (честно):
//   - cwd решения: файлов банка в нём больше нет, штатный приём `cat check.sh`
//     не читает ничего; подставной check.sh/*_test.go/test_*.py из файлов
//     кандидата не исполняется (go — подмена тестом банка через -overlay,
//     python — pytest собирается по каталогу градатора, shell — bash /grade/check.sh);
//   - запись в docker: /grade примонтирован только на чтение — ни `> check.sh`,
//     ни rename/unlink не проходят (права ни при чём — uid контейнера равен uid
//     процесса sandbox, см. --user в runDocker);
//   - запись в subprocess: файлы 0444, перенаправление не проходит; rename и
//     unlink возможны (каталог принадлежит тому же uid) — изоляция недостаточна,
//     режим dev-fallback;
//   - чтение по абсолютному пути (../grade/check.sh, /grade/check.sh) возможно в
//     обоих режимах: код кандидата идёт под тем же uid, что и процесс sandbox.
//     shortcut: реальная read-изоляция требует раздельных uid для процесса
//     кандидата и каталога градатора (прод-режим docker — ближайший шаг к этому);
//     здесь закрыты: утечка из cwd, гарантированный pass и подмена градатора.
func (r *Runner) Run(ctx context.Context, stack string, files, trusted map[string]string) (Result, error) {
	switch stack {
	case "go", "python", "shell":
	default:
		return Result{}, fmt.Errorf("%w: %q (ожидается go|python|shell)", ErrUnsupportedStack, stack)
	}
	if len(files) == 0 {
		return Result{}, fmt.Errorf("%w: пустой набор файлов", ErrBadFiles)
	}
	for path := range files {
		if !safePath(path) {
			return Result{}, fmt.Errorf("%w: %q (пути — относительно, без ..)", ErrBadFiles, path)
		}
	}
	for path := range trusted {
		if !safePath(path) {
			return Result{}, fmt.Errorf("%w: доверенный файл %q (пути — относительно, без ..)", ErrBadFiles, path)
		}
	}

	// spec-14/C3: ограничиваем число одновременных запусков — каждый run
	// порождает тяжёлое дерево процессов (холодный go test ≈ 2 ГБ VA);
	// N одновременных POST /runs без предела → OOM хоста.
	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	case <-ctx.Done():
		return Result{}, fmt.Errorf("semaphore: %w", ctx.Err())
	}

	workdir, err := r.newWorkDir()
	if err != nil {
		return Result{}, fmt.Errorf("workdir: %w", err)
	}
	defer func() { _ = os.RemoveAll(workdir) }()

	runDir := filepath.Join(workdir, "run")
	gradeDir := filepath.Join(workdir, "grade")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return Result{}, fmt.Errorf("mkdir run: %w", err)
	}
	if err := os.MkdirAll(gradeDir, 0o700); err != nil {
		return Result{}, fmt.Errorf("mkdir grade: %w", err)
	}
	if err := writeTree(runDir, files, 0o644); err != nil {
		return Result{}, err
	}
	if err := writeTree(gradeDir, trusted, 0o444); err != nil {
		return Result{}, err
	}

	// Пути, которые видит исполняемая команда: в docker это точки монтирования
	// (/work — на запись, /grade — только чтение), в subprocess — пути хоста.
	candRef, gradeRef := runDir, gradeDir
	if r.cfg.Mode == "docker" {
		candRef, gradeRef = "/work", "/grade"
	}
	if err := writeGoOverlay(gradeDir, gradeRef, candRef, stack, trusted); err != nil {
		return Result{}, err
	}
	cmdLine := strings.ReplaceAll(r.cfg.Commands[stack], graderToken, gradeRef)

	timeout := r.cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
		if stack == "go" {
			timeout = defaultTimeoutGo
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()

	var raw []byte
	var exitCode int
	switch r.cfg.Mode {
	case "subprocess":
		var runErr error
		raw, exitCode, runErr = r.runSubprocess(ctx, stack, runDir, cmdLine)
		if runErr != nil && !errors.Is(runErr, context.DeadlineExceeded) {
			if errors.Is(runErr, ErrNoDocker) {
				return Result{}, runErr
			}
			return Result{}, fmt.Errorf("subprocess: %w", runErr)
		}
	case "docker":
		var runErr error
		raw, exitCode, runErr = r.runDocker(ctx, stack, runDir, gradeDir, cmdLine)
		if runErr != nil && !errors.Is(runErr, context.DeadlineExceeded) {
			return Result{}, runErr
		}
	default:
		return Result{}, fmt.Errorf("неизвестный режим: %q", r.cfg.Mode)
	}

	duration := time.Since(start)
	result := Result{
		ExitCode:   exitCode,
		DurationMS: int(duration.Milliseconds()),
		Timeout:    errors.Is(ctx.Err(), context.DeadlineExceeded) || exitCode == 124,
	}
	if len(raw) > r.cfg.MaxBytes {
		raw = raw[:r.cfg.MaxBytes]
	}
	switch stack {
	case "go":
		result.Stdout, result.Stderr, result.Tests = splitStreams(raw)
		result.Tests = parseGoTests(raw, result.Tests)
	case "python":
		outStr := string(raw)
		result.Stdout = outStr
		// pytest пишет всё в stdout; stderr пуст, если не было сбоев интерпретатора.
		if exitCode != 0 && strings.Contains(outStr, "Error") {
			result.Stderr = outStr
		}
		result.Tests = []TestResult{{Name: "pytest", Passed: exitCode == 0}}
	case "shell":
		// spec-19: вывод чек-скрипта — сырой текст (не JSON, digest-парсер
		// фронтенда пропускает как есть); passed = exit 0 check.sh.
		result.Stdout = string(raw)
		result.Tests = []TestResult{{Name: "check.sh", Passed: exitCode == 0}}
	}
	if result.Timeout {
		result.ExitCode = 124
		result.Stderr += "\n[timeout] запуск прерван по таймауту"
	}
	result.Passed = !result.Timeout && exitCode == 0
	return result, nil
}
