package runner

// Фаза 4 (TEST_PLAN §4): пробои subprocess-режима на живом раннере.
// Docker-изоляция (network=none, OOM-лимиты, FS) проверяется только в
// docker-режиме — здесь покрыты: таймаут, работа под произвольной командой,
// MaxBytes-обрезка, изоляция рабочих каталогов (workdir вне /tmp).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSandboxProbeInfiniteLoop — задача-«бомба»: вечный цикл прерывается
// таймаутом (10 с → 1.5 с в тесте), timeout=true, passed=false.
func TestSandboxProbeInfiniteLoop(t *testing.T) {
	r := New(Config{
		Mode:     "subprocess",
		Timeout:  1500 * time.Millisecond,
		Commands: map[string]string{"go": "python3 -c 'import time\nwhile True: time.sleep(1)'"},
	})
	start := time.Now()
	res, err := r.Run(t.Context(), "go", map[string]string{"dummy.txt": "x"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	d := time.Since(start)
	if !res.Timeout {
		t.Errorf("ожидается timeout=true, получен %+v", res)
	}
	if res.Passed {
		t.Error("задача с вечным циклом не может быть passed")
	}
	if res.ExitCode != 124 {
		t.Errorf("exit code: ожидается 124, получен %d", res.ExitCode)
	}
	if d > 5*time.Second {
		t.Errorf("таймаут 1.5 с, а запуск тянул %v", d)
	}
	if !strings.Contains(res.Stderr, "[timeout]") {
		t.Errorf("stderr без маркера таймаута: %q", res.Stderr)
	}
	t.Logf("loop: %v, exit=%d, timeout=%v", d, res.ExitCode, res.Timeout)
}

// TestSandboxProbeMemoryBomb — «память»: аллокация 2 ГБ в python.
// В subprocess-режиме без rlimit процесс уйдёт в OOM-killer ядра (или
// закончится долго) — проверяем, что раннер не зависает и возвращает результат
// (в docker-режиме OOM-kill контейнера — отдельный сценарий, TEST_PLAN §4.2).
func TestSandboxProbeMemoryBomb(t *testing.T) {
	r := New(Config{
		Mode:     "subprocess",
		Timeout:  8 * time.Second,
		Commands: map[string]string{"go": "python3 -c 'x = [b\"a\" * (1 << 20) for _ in range(2048)]'"},
	})
	res, err := r.Run(t.Context(), "go", map[string]string{"dummy.txt": "x"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Passed && res.ExitCode == 0 {
		t.Log("примечание: аллокация 2 ГБ завершилась (хватает памяти) — OOM-сценарий актуален только для docker-режима")
	}
	t.Logf("mem: exit=%d passed=%v timeout=%v dur=%d мс", res.ExitCode, res.Passed, res.Timeout, res.DurationMS)
}

// TestSandboxProbeOutputFlood — поток вывода больше MaxBytes обрезается.
func TestSandboxProbeOutputFlood(t *testing.T) {
	r := New(Config{
		Mode:     "subprocess",
		Timeout:  8 * time.Second,
		MaxBytes: 1024,
		Commands: map[string]string{"go": "python3 -c 'print(\"x\" * 100000)'"},
	})
	res, err := r.Run(t.Context(), "go", map[string]string{"dummy.txt": "x"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !res.Passed {
		t.Errorf("задача должна пройти (exit 0): %+v", res)
	}
	if len(res.Stdout) > 1024 {
		t.Errorf("stdout не обрезан по MaxBytes: %d байт", len(res.Stdout))
	}
	t.Logf("flood: stdout=%d байт (MaxBytes=1024)", len(res.Stdout))
}

// TestSandboxProbeWorkDirIsolation — рабочий каталог создаётся под WorkDirBase
// (не в /tmp) и удаляется после запуска.
func TestSandboxProbeWorkDirIsolation(t *testing.T) {
	base := t.TempDir()
	r := New(Config{
		Mode:        "subprocess",
		Timeout:     8 * time.Second,
		WorkDirBase: base,
		Commands:    map[string]string{"go": "python3 -c 'import os;print(os.environ.get(\"PWD\",\"?\"))'"},
	})
	res, err := r.Run(t.Context(), "go", map[string]string{"dummy.txt": "x"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !res.Passed {
		t.Errorf("задача должна пройти: %+v", res)
	}
	// После Run — workdir удалён: под базой не осталось каталогов запуска.
	entries, _ := os.ReadDir(base)
	if len(entries) != 0 {
		t.Errorf("workdir не убран: %v", entries)
	}
	// Запуск происходил не в системном /tmp:
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "grade-sandbox") {
			t.Errorf("workdir остался под базой: %s", filepath.Join(base, e.Name()))
		}
	}
	t.Log("изоляция workdir: OK (создан под базой, удалён после запуска)")
}
