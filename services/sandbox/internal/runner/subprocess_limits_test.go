package runner

// RLIMIT subprocess-режима (T-20261009133832): память (RLIMIT_AS) и CPU
// (RLIMIT_CPU) — аналог лимитов docker-режима (ADR-003). Тесты: 2 ГБ
// аллокация не виснет (MemoryError/exit), вечный цикл — timeout (SIGXCPU →
// 124), регресс банка задач в subprocess-режиме.

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/sandbox/internal/tasks"
)

// TestSubprocessMemoryLimit — аллокация 2 ГБ при RLIMIT_AS=512 МБ: процесс
// падает с MemoryError (exit != 0) быстро, раннер не виснет (до rlimit процесс
// уходил в OOM-killer ядра — см. TestSandboxProbeMemoryBomb).
func TestSubprocessMemoryLimit(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("нет python3")
	}
	r := New(Config{
		Mode:     "subprocess",
		Timeout:  10 * time.Second,
		Commands: map[string]string{"python": `python3 -c 'x = [b"a" * (1 << 20) for _ in range(2048)]'`},
	})
	start := time.Now()
	res, err := r.Run(t.Context(), "python", map[string]string{"dummy.txt": "x"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if d := time.Since(start); d > 7*time.Second {
		t.Fatalf("аллокация 2 ГБ виснет (%v) — rlimit не работает", d)
	}
	if res.Passed || res.ExitCode == 0 {
		t.Fatalf("2 ГБ при лимите 512 МБ не должно пройти: %+v", res)
	}
	joined := res.Stdout + res.Stderr
	if !strings.Contains(joined, "MemoryError") {
		t.Logf("примечание: без маркера MemoryError (exit=%d): %q", res.ExitCode, joined[:min(len(joined), 120)])
	}
	t.Logf("mem-limit: exit=%d passed=%v dur=%v (маркер MemoryError: %v)",
		res.ExitCode, res.Passed, time.Since(start), strings.Contains(joined, "MemoryError"))
}

// TestSubprocessCPULimit — вечный цикл при RLIMIT_CPU=2 с (wall-таймаут 60 с):
// процесс убивается исчерпанием CPU-лимита (SIGXCPU) за ~2 с — результат
// маркируется timeout/124, раннер не ждёт wall-таймаут.
func TestSubprocessCPULimit(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("нет python3")
	}
	r := New(Config{
		Mode:     "subprocess",
		Timeout:  60 * time.Second,
		CPUSecs:  2,
		Commands: map[string]string{"python": "python3 -c 'while True: pass'"},
	})
	start := time.Now()
	res, err := r.Run(t.Context(), "python", map[string]string{"dummy.txt": "x"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	d := time.Since(start)
	if d > 15*time.Second {
		t.Fatalf("вечный цикл тянулся %v — RLIMIT_CPU не работает (ожидалось ~2-4 с)", d)
	}
	if !res.Timeout {
		t.Fatalf("ожидается timeout=true, получен %+v", res)
	}
	if res.Passed {
		t.Error("вечный цикл не может быть passed")
	}
	if res.ExitCode != 124 {
		t.Errorf("ожидается exit 124, получен %d", res.ExitCode)
	}
	t.Logf("cpu-limit: %v, exit=%d, timeout=%v", d, res.ExitCode, res.Timeout)
}

// TestBankSubprocessRegression — регресс: все задачи банка исполняются в
// subprocess-режиме (с rlimit) как раньше: раннер завершается без ошибок,
// stub-решения проваливаются тестами банка (passed=false), тесты распарсены.
// Размер банка не хардкодим (банк расширяется) — проверяем, что он непуст и
// содержит оба стека (go + python). Нужен go-интерпретатор и python3.
func TestBankSubprocessRegression(t *testing.T) {
	if testing.Short() {
		t.Skip("нужны go/python3 интерпретаторы (не -short)")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("нет go-интерпретатора")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("нет python3")
	}
	bank, err := tasks.Default()
	if err != nil {
		t.Fatalf("tasks: %v", err)
	}
	tasksList := bank.List("", "")
	if len(tasksList) == 0 {
		t.Fatalf("банк пуст")
	}
	goN, pyN := 0, 0
	for _, task := range tasksList {
		switch task.Stack {
		case "go":
			goN++
		case "python":
			pyN++
		}
	}
	if goN == 0 || pyN == 0 {
		t.Fatalf("банк должен содержать и go, и python задачи (go=%d, python=%d)", goN, pyN)
	}
	t.Logf("банк: %d задач (go=%d, python=%d)", len(tasksList), goN, pyN)
	r := New(Config{Mode: "subprocess"})
	for _, task := range tasksList {
		res, err := r.Run(t.Context(), task.Stack, task.Files)
		if err != nil {
			t.Fatalf("%s: run: %v", task.ID, err)
		}
		// Bank-задачи — stub-решения (TODO): исполнение обязательно проваливается
		// тестами банка (кандидат должен реализовать решение).
		if res.Passed {
			t.Errorf("%s: stub-задача прошла (ожидается passed=false): %+v", task.ID, res)
		}
		if res.Timeout {
			t.Errorf("%s: timeout на stub-задаче: %+v", task.ID, res)
		}
		switch task.Stack {
		case "go":
			if len(res.Tests) == 0 && res.ExitCode == 0 {
				t.Errorf("%s: ни тестов, ни ошибки сборки — подстановка банка не подтверждена", task.ID)
			}
		case "python":
			if len(res.Tests) != 1 || res.Tests[0].Name != "pytest" {
				t.Errorf("%s: pytest-тест не распарсен: %+v", task.ID, res.Tests)
			}
		}
		t.Logf("%s: exit=%d tests=%d dur=%d мс", task.ID, res.ExitCode, len(res.Tests), res.DurationMS)
	}
}
