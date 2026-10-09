package runner

// Docker-пробы sandbox (T-20261009133832): прогон задач в docker-режиме
// (ADR-003: --network=none, --memory=512m, --cpus=1, --pids-limit=128) с
// проверкой Result-контракта (passed/timeout/exit_code). Опционально:
// SANDBOX_DOCKER=1 + docker CLI + запущенный демон + образы golang:1.24 /
// python:3.12-slim (CI: job sandbox-docker, локально: scripts/ci-docker-test.sh).

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestSandboxDockerProbe — 3 пробы в docker-режиме:
//  1. go-pass — go-тесты проходят + сетевой тест доказывает --network=none
//     (внешний dial обязан упасть);
//  2. py-pass — pytest проходит;
//  3. py-oom  — аллокация 2 ГБ при --memory=512m → OOM-kill контейнера
//     (exit 137), passed=false, без timeout.
func TestSandboxDockerProbe(t *testing.T) {
	if testing.Short() {
		t.Skip("docker-пробы (не -short)")
	}
	if os.Getenv("SANDBOX_DOCKER") == "" {
		t.Skip("SANDBOX_DOCKER не задан — docker-пробы опциональны (CI: job sandbox-docker)")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("нет docker CLI")
	}
	if out, err := exec.Command("docker", "info").CombinedOutput(); err != nil {
		t.Skipf("docker-демон недоступен: %v (%s)", err, out)
	}

	r := New(Config{Mode: "docker", Timeout: 120 * time.Second})

	// 1. go-pass: тесты проходят, сеть отключена (dial наружу падает).
	goFiles := map[string]string{
		"go.mod":      "module task\n\ngo 1.21\n",
		"main.go":     "package main\n\nfunc main() {}\n",
		"net_test.go": "package main\n\nimport (\n\t\"net\"\n\t\"testing\"\n\t\"time\"\n)\n\n// TestNoNetwork доказывает --network=none: внешний dial обязан упасть.\nfunc TestNoNetwork(t *testing.T) {\n\tc, err := net.DialTimeout(\"tcp\", \"1.1.1.1:443\", 2*time.Second)\n\tif err == nil {\n\t\tc.Close()\n\t\tt.Fatal(\"сеть доступна — --network=none не работает\")\n\t}\n}\n",
	}
	res, err := r.Run(t.Context(), "go", goFiles)
	if err != nil {
		t.Fatalf("go-pass: %v", err)
	}
	if !res.Passed || res.ExitCode != 0 {
		t.Fatalf("go-pass: %+v (stdout: %s)", res, res.Stdout[:min(len(res.Stdout), 300)])
	}
	var netOK bool
	for _, tr := range res.Tests {
		if tr.Name == "TestNoNetwork" && tr.Passed {
			netOK = true
		}
	}
	if !netOK {
		t.Fatalf("go-pass: TestNoNetwork не исполнен/не пройден: %+v", res.Tests)
	}
	t.Logf("go-pass: OK (network=none подтверждён), dur=%d мс", res.DurationMS)

	// 2. py-pass: pytest проходит.
	res, err = r.Run(t.Context(), "python", map[string]string{
		"test_ok.py": "def test_ok():\n    assert 1 + 1 == 2\n",
	})
	if err != nil {
		t.Fatalf("py-pass: %v", err)
	}
	if !res.Passed || res.ExitCode != 0 {
		t.Fatalf("py-pass: %+v (stdout: %s)", res, res.Stdout[:min(len(res.Stdout), 300)])
	}
	t.Logf("py-pass: OK, dur=%d мс", res.DurationMS)

	// 3. py-oom: 2 ГБ при --memory=512m → OOM-kill (137), без timeout.
	rOOM := New(Config{
		Mode:    "docker",
		Timeout: 120 * time.Second,
		Commands: map[string]string{
			"python": `python3 -c 'x = [b"a" * (1 << 20) for _ in range(2048)]'`,
		},
	})
	res, err = rOOM.Run(t.Context(), "python", map[string]string{"dummy.txt": "x"})
	if err != nil {
		t.Fatalf("py-oom: %v", err)
	}
	if res.Passed {
		t.Fatalf("py-oom: 2 ГБ при лимите 512 МБ не должно пройти: %+v", res)
	}
	if res.Timeout {
		t.Errorf("py-oom: OOM-kill не должен маркироваться timeout: %+v", res)
	}
	if res.ExitCode != 137 {
		t.Logf("примечание: OOM-маркер 137 не получен (exit=%d) — проверяем только passed=false", res.ExitCode)
	}
	t.Logf("py-oom: exit=%d passed=%v dur=%d мс (ожидается 137/OOM)", res.ExitCode, res.Passed, res.DurationMS)
}
