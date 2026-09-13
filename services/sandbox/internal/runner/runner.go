// Package runner — выполнение кода кандидата (WP-6, ADR-003).
//
// Режимы:
//   - "subprocess" (dev): честный интерпретатор хоста, изолированный cwd,
//     минимальный env, таймаут, ограничение вывода. Только для разработки.
//   - "docker" (prod): docker run --rm с лимитами ADR-003
//     (--network=none, --cpus=1, --memory=512m, --pids-limit=128, --read-only,
//     --tmpfs, --user 1000:1000).
//
// Отклонение от ADR-003 (зафиксировано в PROJECT_MEMORY): WP-6 — контейнер
// на каждый run (упрощение MVP); long-lived контейнер на сессию — бэклог Operations.
package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Пороки по умолчанию (ADR-003: результат ≤ 10 с).
const (
	DefaultTimeout  = 10 * time.Second
	DefaultMaxBytes = 1 << 20 // 1 МБ на stdout/stderr
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
	// Commands: stack → команда тестов в subprocess-режиме
	// (по умолчанию: go — "go test -count=1 -json ./...", python — "python3 -m pytest -q").
	// Тестируется подменой на произвольные команды.
	Commands map[string]string
	// WorkDirBase — корень рабочих каталогов (по умолчанию $SANDBOX_WORKDIR_BASE
	// или ~/.local/share/grade-sandbox; НЕ /tmp — см. newWorkDir).
	WorkDirBase string
}

// Runner — выполнение запусков.
type Runner struct {
	cfg Config
}

// New создаёт раннер (режим и пороки — из конфига).
func New(cfg Config) *Runner {
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = DefaultMaxBytes
	}
	if cfg.Images == nil {
		cfg.Images = map[string]string{"go": "golang:1.24", "python": "python:3.12-slim"}
	}
	if cfg.Commands == nil {
		cfg.Commands = map[string]string{
			"go":     "go test -count=1 -json ./...",
			"python": "python3 -m pytest -q",
		}
	}
	return &Runner{cfg: cfg}
}

// Mode — активный режим.
func (r *Runner) Mode() string { return r.cfg.Mode }

// Run выполняет тесты файлов задачи (action "test").
func (r *Runner) Run(ctx context.Context, stack string, files map[string]string) (Result, error) {
	switch stack {
	case "go", "python":
	default:
		return Result{}, fmt.Errorf("%w: %q (ожидается go|python)", ErrUnsupportedStack, stack)
	}
	if len(files) == 0 {
		return Result{}, fmt.Errorf("%w: пустой набор файлов", ErrBadFiles)
	}
	for path := range files {
		if !safePath(path) {
			return Result{}, fmt.Errorf("%w: %q (пути — относительно, без ..)", ErrBadFiles, path)
		}
	}

	workdir, err := r.newWorkDir()
	if err != nil {
		return Result{}, fmt.Errorf("workdir: %w", err)
	}
	defer func() { _ = os.RemoveAll(workdir) }()
	for path, content := range files {
		full := filepath.Join(workdir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return Result{}, fmt.Errorf("mkdir %s: %w", path, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			return Result{}, fmt.Errorf("write %s: %w", path, err)
		}
	}

	ctx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	defer cancel()
	start := time.Now()

	var raw []byte
	var exitCode int
	switch r.cfg.Mode {
	case "subprocess":
		var runErr error
		raw, exitCode, runErr = r.runSubprocess(ctx, stack, workdir)
		if runErr != nil && !errors.Is(runErr, context.DeadlineExceeded) {
			if errors.Is(runErr, ErrNoDocker) {
				return Result{}, runErr
			}
			return Result{}, fmt.Errorf("subprocess: %w", runErr)
		}
	case "docker":
		var runErr error
		raw, exitCode, runErr = r.runDocker(ctx, stack, workdir)
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
	}
	if result.Timeout {
		result.ExitCode = 124
		result.Stderr += "\n[timeout] запуск прерван по таймауту"
	}
	result.Passed = !result.Timeout && exitCode == 0
	return result, nil
}

// newWorkDir — рабочий каталог запуска.
// ВАЖНО: не в системном temp-root (/tmp) — Go игнорирует go.mod в /tmp
// ("ignoring go.mod in system temp root"), и `go test` не может стартовать.
func (r *Runner) newWorkDir() (string, error) {
	base := r.cfg.WorkDirBase
	if base == "" {
		base = os.Getenv("SANDBOX_WORKDIR_BASE")
	}
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("home dir: %w", err)
		}
		base = filepath.Join(home, ".local", "share", "grade-sandbox")
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", fmt.Errorf("workdir base: %w", err)
	}
	return os.MkdirTemp(base, "run-*")
}

// safePath — путь относительно, без обхода вверх.
func safePath(p string) bool {
	if p == "" || filepath.IsAbs(p) {
		return false
	}
	clean := filepath.Clean(filepath.FromSlash(p))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

// runSubprocess — dev-режим: честный интерпретатор хоста в изолированном cwd.
func (r *Runner) runSubprocess(ctx context.Context, stack, workdir string) ([]byte, int, error) {
	cmdLine := r.cfg.Commands[stack]
	var cmd *exec.Cmd
	switch stack {
	case "go":
		cmd = exec.CommandContext(ctx, "sh", "-c", cmdLine)
	case "python":
		cmd = exec.CommandContext(ctx, "sh", "-c", cmdLine)
	}
	cmd.Dir = workdir
	cmd.Env = cleanEnv(workdir, stack)
	var out bytes.Buffer
	capped := &cappedWriter{buf: &out, limit: int64(r.cfg.MaxBytes * 2)}
	cmd.Stdout = capped
	cmd.Stderr = capped
	// Своя группа процессов: по таймауту убиваем всю группу (go/pytest запускают
	// дочерние процессы), а не только родительский sh.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, -1, fmt.Errorf("start: %w", err)
	}
	err := cmd.Wait()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		return out.Bytes(), 124, context.DeadlineExceeded
	}
	exitCode := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exitCode = ee.ExitCode()
		} else {
			return out.Bytes(), -1, err
		}
	}
	return out.Bytes(), exitCode, nil
}

// runDocker — prod-режим: контейнер на run с лимитами ADR-003.
func (r *Runner) runDocker(ctx context.Context, stack, workdir string) ([]byte, int, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, -1, ErrNoDocker
	}
	image := r.cfg.Images[stack]
	cmdLine := r.cfg.Commands[stack]
	// ENV для /tmp (rootfs read-only): HOME + кэши Go.
	env := []string{
		"HOME=/tmp", "TMPDIR=/tmp",
		"GOCACHE=/tmp/gocache", "GOPATH=/tmp/gopath", "GOTOOLCHAIN=local", "GOFLAGS=-mod=mod",
	}
	args := []string{
		"run", "--rm",
		"--network=none",
		"--cpus=1",
		"--memory=512m",
		"--pids-limit=128",
		"--read-only",
		"--tmpfs", "/tmp:size=128m",
		"--user", "1000:1000",
		"-v", workdir + ":/work:rw",
	}
	for _, e := range env {
		args = append(args, "-e", e)
	}
	args = append(args, image, "sh", "-c", "cd /work && "+cmdLine)

	cmd := exec.CommandContext(ctx, "docker", args...)
	var out bytes.Buffer
	capped := &cappedWriter{buf: &out, limit: int64(r.cfg.MaxBytes * 2)}
	cmd.Stdout = capped
	cmd.Stderr = capped
	if err := cmd.Start(); err != nil {
		if strings.Contains(err.Error(), "executable file not found") {
			return nil, -1, ErrNoDocker
		}
		return nil, -1, fmt.Errorf("docker start: %w", err)
	}
	err := cmd.Wait()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return out.Bytes(), 124, context.DeadlineExceeded
	}
	exitCode := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exitCode = ee.ExitCode()
		} else {
			return out.Bytes(), -1, fmt.Errorf("docker: %w", err)
		}
	}
	return out.Bytes(), exitCode, nil
}

// cleanEnv — минимальное окружение subprocess-режима (честный, но без хост-секретов).
func cleanEnv(workdir, stack string) []string {
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + workdir,
		// TMPDIR = workdir НЕ ставим: Go считает $TMPDIR «системным temp-root»
		// и игнорирует go.mod внутри него (golang.org/issue/26708).
		"LANG=C.UTF-8",
	}
	if stack == "go" {
		env = append(env,
			"GOCACHE="+workdir+"/.gocache",
			"GOPATH="+workdir+"/.gopath",
			"GOTOOLCHAIN=local",
			"GOFLAGS=-mod=mod",
			"GOPROXY=off", // ADR-003: без сети — зависимости из банка задач
		)
	}
	if v := os.Getenv("PYTHONPATH"); v != "" {
		env = append(env, "PYTHONPATH="+v) // dev: тестовые зависимости хоста (в prod — docker-образ)
	}
	return env
}

// parseGoTests — из `go test -json`: имя теста → итоговый pass/fail.
func parseGoTests(raw []byte, fallback []TestResult) []TestResult {
	type ev struct {
		Action string
		Test   string
	}
	tests := map[string]bool{}
	order := []string{}
	rest := raw
	for len(rest) > 0 {
		var line []byte
		line, rest, _ = bytes.Cut(rest, []byte("\n"))
		var e ev
		if json.Unmarshal(line, &e) != nil || e.Test == "" {
			continue
		}
		switch e.Action {
		case "pass", "fail", "skip":
			passed := e.Action != "fail"
			if _, seen := tests[e.Test]; !seen {
				order = append(order, e.Test)
			}
			tests[e.Test] = passed
		}
	}
	if len(order) == 0 {
		return fallback
	}
	out := make([]TestResult, 0, len(order))
	for _, name := range order {
		out = append(out, TestResult{Name: name, Passed: tests[name]})
	}
	return out
}

// splitStreams — `go test -json` пишет всё в stdout; stdout/stderr разделяем
// эвристики нет — оба поля получают один и тот же поток (контракт не сломан).
func splitStreams(raw []byte) (string, string, []TestResult) {
	return string(raw), "", nil
}

// cappedWriter — ограничитель размера вывода (защита от OOM при бесконечном печатании).
type cappedWriter struct {
	buf   *bytes.Buffer
	limit int64
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	if int64(w.buf.Len()) >= w.limit {
		return len(p), nil // глотаем остаток, счётчик возвращаем честно
	}
	w.buf.Write(p)
	return len(p), nil
}
