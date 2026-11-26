// docker — prod-режим: docker run с лимитами ADR-003, прогрев образов
// и минимальное окружение прогонов (вынесено из runner.go).
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// WarmupImage — предзатягивание образа стека в docker-режиме (spec-20):
// первый запуск задачи с холодным образом тянет pull на время кандидата
// (debian-slim ~30 МБ, golang ~350 МБ) — «первый кандидат ждёт минуту».
// Прогрев на старте сервиса (асинхронно) убирает холодный pull. Возвращает
// ошибку по-образу (run сам сделает pull при отсутствии — прогрев best-effort).
func (r *Runner) WarmupImage(ctx context.Context, stack string) error {
	if r.cfg.Mode != "docker" {
		return nil
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return ErrNoDocker
	}
	img := r.cfg.Images[stack]
	if img == "" {
		return nil
	}
	pullCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(pullCtx, "docker", "pull", "--quiet", img)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("pull %s: %w: %s", img, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// runDocker — prod-режим: контейнер на run с лимитами ADR-003. Каталог
// градатора монтируется отдельно и только на чтение (/grade) — код кандидата
// живёт в /work и не может ни изменить, ни найти файлы банка в своём дереве
// (BUG-20261122-033).
func (r *Runner) runDocker(ctx context.Context, stack, runDir, gradeDir, cmdLine string) ([]byte, int, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, -1, ErrNoDocker
	}
	image := r.cfg.Images[stack]
	// ENV для /tmp (rootfs read-only): HOME + кэши Go. spec-14/C6: GOPROXY=off —
	// с --network=none попытка скачать зависимости даёт честную ошибку вместо
	// сетевого зависания dial.
	// BUG-20261103-023: GOCACHE переехал с /tmp (tmpfs 128m) на bind-mount
	// workdir: холодная сборка нет-стека (net + crypto, ~150 МБ кэша) влезала
	// в tmpfs только на старых версиях образа golang:1.24 — на свежем linker
	// падал «mapping output file failed: no space left on device». Bind-mount
	// — дисковый (не RAM), зачищается с workdir после прогона. CGO_ENABLED=0 —
	// сборка без gcc (в 2-3 раза меньше кэш; cgo-задачи не поддерживались
	// и раньше: GOPROXY=off).
	cacheMounts, goCache := r.goCacheRefs(stack)
	env := []string{
		"HOME=/tmp", "TMPDIR=/tmp",
		"GOCACHE=" + goCache, "GOPATH=/tmp/gopath", "GOTOOLCHAIN=local",
		"GOWORK=off", // go.work хоста не должен включать сборку в workspace mode
		// -p=2: при сборке std на много-ядерном хосте параллельные compile не влезают
		// в --pids-limit/--memory (BUG-20261122-035)
		"GOFLAGS=-mod=mod -p=2",
		"GOPROXY=off", "CGO_ENABLED=0",
	}
	// spec-14/C1: раньше --user 1000:1000 при uid хоста ≠ 1000 не мог ни читать,
	// ни писать /work (MkdirTemp — 0700, владелец — uid хоста) → КАЖДЫЙ run падал
	// (permission denied). Мапим контейнерного пользователя на uid/gid процесса
	// sandbox (хост-пользователя) — каталоги 0700 полностью доступны.
	userOpt := fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
	// spec-14/C4: cidfile — по таймауту CommandContext убивал только клиент
	// `docker run`, контейнер продолжал жить на daemon (утечка CPU/памяти,
	// --rm копился). По ctx.Done() — явный `docker kill <cid>`.
	cidFile, err := os.CreateTemp("", "grade-sandbox-cid-*")
	if err != nil {
		return nil, -1, fmt.Errorf("cidfile: %w", err)
	}
	cidPath := cidFile.Name()
	cidFile.Close()
	defer func() { _ = os.Remove(cidPath) }()
	// BUG-20261103-022: docker создаёт cidfile сам; если файл УЖЕ существует
	// (пустой тем более), современный CLI (27+) отказывает: «container ID file
	// found…» exit 125 — в docker-режиме падал КАЖДЫЙ запуск (на старом
	// docker 20.x пустой файл проходил, поэтому проба T-20261009133832 была
	// зелёной). Файл нужен только как уникальное имя — удаляем до запуска.
	if err := os.Remove(cidPath); err != nil {
		return nil, -1, fmt.Errorf("cidfile: %w", err)
	}
	args := []string{
		"run", "--rm",
		"--network=none",
		"--cpus=1",
		"--memory=512m",
		"--pids-limit=128",
		"--read-only",
		// BUG-20261103-024: docker --tmpfs по умолчанию noexec — собранный
		// go тестовый бинарник на /tmp не исполнялся («fork/exec … permission
		// denied»). Явно разрешаем exec: контейнер одноразовый, за
		// pids/mem/network-лимитами.
		"--tmpfs", "/tmp:size=128m,exec",
		"--user", userOpt,
		"--cidfile", cidPath,
		// spec-14/C6: минимальные привилегии контейнера.
		"--cap-drop=ALL",
		"--security-opt", "no-new-privileges",
		"-v", runDir + ":/work:rw",
		// Градатор — вне /work и только на чтение: подмена чека из решения
		// кандидата невозможна на уровне монтирования (права не спасли бы —
		// --user ниже это uid хоста).
		"-v", gradeDir + ":/grade:ro",
	}
	args = append(args, cacheMounts...)
	for _, e := range env {
		args = append(args, "-e", e)
	}
	args = append(args, image, "sh", "-c", "cd /work && "+cmdLine)

	// spec-14/C4: exec.Command (не CommandContext) — CommandContext убивал
	// только клиент `docker run`, контейнер продолжал жить на daemon.
	// По ctx.Done(): явный `docker kill <cid>` + завершение клиента.
	cmd := exec.Command("docker", args...)
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
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	select {
	case err := <-waitDone:
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
	case <-ctx.Done():
		// Контейнер жив на daemon: убиваем по cid (--rm удалит его сам).
		if cid, err := os.ReadFile(cidPath); err == nil {
			if tr := strings.TrimSpace(string(cid)); tr != "" {
				_ = exec.Command("docker", "kill", tr).Run()
			}
		}
		if cmd.Process != nil {
			_ = cmd.Process.Kill() // завершить висящий клиент docker run
		}
		<-waitDone
		return out.Bytes(), 124, context.DeadlineExceeded
	}
}

// goCacheRefs — как docker-прогон получает кэш сборки go. Без конфига — каталог
// в рабочем каталоге (per-run: std пересобирается каждый раз, ~40 с). С конфигом
// — общий том: значение SANDBOX_GO_CACHE передаётся в `docker run -v` как ЕСТЬ,
// поэтому это именно ИМЯ docker-тома (путь без «/» демон трактует как volume),
// а не путь хоста.
//
// Права: том инициализируется содержимым и владельцем каталога в образе в момент
// первого монтирования, поэтому в образе песочницы /cache принадлежит пользователю
// песочницы (см. sandbox.Dockerfile) — иначе том достался бы root'у и go не смог
// бы в него писать под --user.
//
// Что это НЕ гарантирует: кэш общий для всех кандидатов. Go ключует записи по
// хэшу исходников и тулчейна, поэтому повлиять на чужой результат нельзя,
// а забить диск мусорными записями — можно. shortcut: порог у go сам,
// размусоривание лечится периодическим `docker volume rm`.
func (r *Runner) goCacheRefs(stack string) (mounts []string, cacheDir string) {
	if stack != "go" {
		return nil, "/work/.gocache"
	}
	if r.cfg.GoCache == "" {
		return nil, "/work/.gocache"
	}
	return []string{"-v", r.cfg.GoCache + ":/cache"}, "/cache/go-build"
}
