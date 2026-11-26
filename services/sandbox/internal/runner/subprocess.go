// subprocess — dev-режим запуска: интерпретатор хоста с ulimit-лимитами
// (вынесено из runner.go).
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// runSubprocess — dev-режим: честный интерпретатор хоста в изолированном cwd.
// RLIMIT (аналог docker ADR-003) — через sh-proлог `ulimit` перед командой:
// ограничения наследуются всем дочерним процессам, ставятся детерминированно
// (без гонки и без parent-состояния). Примечание: syscall.SysProcAttr в
// Go stdlib не имеет Rlimit (только x/sys/unix, который не совместим с
// os/exec) — ulimit-пролог даёт те же семантики без зависимостей.
func (r *Runner) runSubprocess(ctx context.Context, stack, workdir, cmdLine string) ([]byte, int, error) {
	memLimit := r.cfg.MemLimitBytes
	if memLimit <= 0 {
		memLimit = defaultMemLimitPython
		if stack == "go" {
			memLimit = defaultMemLimitGo
		}
		if stack == "shell" {
			memLimit = defaultMemLimitShell
		}
	}
	cpuSecs := r.cfg.CPUSecs
	if cpuSecs <= 0 {
		cpuSecs = defaultCPUSecs
	}
	// ulimit -v — виртуальная память (KB), ulimit -t — CPU-время (с), -n —
	// число открытых файлов. spec-14/C2: `|| exit 97` — раньше при провале
	// ulimit (запрошено > hard limit) sh печатал ошибку и ПРОДОЛЖАЛ без
	// лимитов (молчаливый неограниченный запуск). -n (nofile) — best-effort:
	// hard-лимит хоста может быть ниже запрошенного. Fork-bomb (pids) в
	// subprocess-режиме не ограничиваем сознательно: RLIMIT_NPROC per-uid
	// на busy-хосте ломает легитимные запуски (go test порождает дерево
	// процессов) — защита pids за docker-режимом (--pids-limit).
	// БЕЗ exec: exec ломает builtin-команды (exit/cd) и составные («&&»):
	// ограничения установлены в sh ДО команды и наследуются ей.
	//
	// spec-19 (shell): ulimit -u (RLIMIT_NPROC) для shell-стека СОЗНАТЕЛЬНО
	// не ставится — тот же per-uid аргумент, что и для go/python: лимит
	// действует на ВСЕ процессы uid sandbox, а не на один запуск (на busy-
	// хосте с сервисами того же uid понижение NPROC ломает их fork'и).
	// Fork-bomb в bash-задаче гасится двумя механизмами: group-kill по
	// таймауту (ниже, Setpgid+SIGKILL — дети умирают с родителем) и
	// prod-режимом docker (--pids-limit=128, сеть off).
	cmdLine = fmt.Sprintf(
		"ulimit -v %d || exit 97; ulimit -t %d || exit 97;"+
			" ulimit -n 512 2>/dev/null || ulimit -n $(ulimit -n -H) 2>/dev/null || true; %s",
		memLimit/1024, cpuSecs, cmdLine)
	// ВАЖНО: exec.Command (не CommandContext): CommandContext убивает только
	// родительский sh, а пайп stdout/stderr продолжат удерживать дочерние
	// процессы (python/go-компилятор) — cmd.Wait() будет ждать EOF по пайпу
	// вечно (мёртвый цикл, найден пробами Фазы 4). Поэтому по истечении ctx
	// убиваем всю группу процессов (Setpgid) ДО завершения Wait.
	cmd := exec.Command("sh", "-c", cmdLine)
	cmd.Dir = workdir
	cmd.Env = cleanEnv(workdir, stack)
	if r.cfg.GoCache != "" {
		// GOCACHE перекрывает per-run значение (os/exec оставляет последнее
		// вхождение переменной).
		cmd.Env = append(cmd.Env, "GOCACHE="+r.cfg.GoCache)
	}
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
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	select {
	case err := <-waitDone:
		if err == nil {
			return out.Bytes(), 0, nil
		}
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			return out.Bytes(), -1, err
		}
		if isResourceLimitKill(ee) {
			// Исчерпан RLIMIT (CPU: SIGXCPU/принудительный SIGKILL ядра после 110%
			// лимита; память: ядро) — семантически «таймаут»: 124/timeout.
			return out.Bytes(), 124, context.DeadlineExceeded
		}
		return out.Bytes(), ee.ExitCode(), nil
	case <-ctx.Done():
		// Убиваем группу (SIGKILL) — пайпы закроются, Wait завершится.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-waitDone
		return out.Bytes(), 124, context.DeadlineExceeded
	}
}

// isResourceLimitKill — процесс убит исчерпанием RLIMIT:
//   - 128+SIGXCPU: обёртка sh передала код «дочерний убит сигналом» (первый
//     порог RLIMIT_CPU; CPython перехватывает SIGXCPU и продолжает — тогда
//     ядро убивает SIGKILL);
//   - 128+SIGKILL (137): принудительное убийство ядра при превышении лимита
//     на >10% (RLIMIT_CPU) или OOM; в subprocess-режиме с RLIMIT_AS OOM-killer
//     маловероятен (аллокации падают MemoryError раньше), поэтому 137 считаем
//     исчерпанием ресурса. Кандидатский os.kill(self, SIGKILL) тоже даёт 137 —
//     принимается как «запуск прерван» (known limitation, зафиксировано);
//   - exit -1 + SIGXCPU: сам cmd (sh) убит сигналом напрямую.
//
// Номера сигналов — из syscall (портильно).
func isResourceLimitKill(ee *exec.ExitError) bool {
	code := ee.ExitCode()
	if code == 128+int(syscall.SIGXCPU) || code == 128+int(syscall.SIGKILL) {
		return true
	}
	if code == -1 {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() && ws.Signal() == syscall.SIGXCPU {
			return true
		}
	}
	return false
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
			// go.work родителя (дерево, откуда запущен сервис) переводит сборку в
			// workspace mode, и GOFLAGS=-mod=mod становится ошибкой: «-mod may
			// only be set to readonly or vendor when in workspace mode». Решение
			// кандидата не должно зависеть от того, где лежит репозиторий.
			"GOWORK=off",
			"GOFLAGS=-mod=mod -p=2", // -p=2: 28 параллельных compile не влезают в RLIMIT_AS (см. BUG-20261122-035)
			"GOPROXY=off",           // ADR-003: без сети — зависимости из банка задач
		)
	}
	if v := os.Getenv("PYTHONPATH"); v != "" {
		env = append(env, "PYTHONPATH="+v) // dev: тестовые зависимости хоста (в prod — docker-образ)
	}
	return env
}
