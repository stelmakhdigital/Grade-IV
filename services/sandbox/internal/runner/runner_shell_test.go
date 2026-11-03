package runner

// spec-19: shell-задания сисадмина (практическая стадия некодовых треков).
// Subprocess-режим — bash на хосте (dev); docker-режим — debian:12-slim с
// лимитами ADR-003 (fork-bomb за --pids-limit). Пробы docker — опционально
// (SANDBOX_DOCKER=1, как TestSandboxDockerProbe).

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// sumTaskFiles — задача shell-sum-avg: решение кандидата + чек-скрипт банка.
func sumTaskFiles(solution string) map[string]string {
	return map[string]string{
		"solution.sh": solution,
		"check.sh": `#!/usr/bin/env bash
set -u
fail() { echo "FAIL: $1" >&2; exit 1; }
OUT=$(printf '1 2 3\n4\n' | bash solution.sh) || fail "решение завершилось с ошибкой"
echo "$OUT" | grep -qx 'sum=10' || fail "ожидалась строка sum=10"
echo "$OUT" | grep -qx 'avg=2' || fail "ожидалась строка avg=2 (целочисленное деление 10/4)"
echo OK
`,
	}
}

// TestShellSubprocessRun — корректное решение проходит чек-скрипт:
// passed=true, tests=[check.sh], stdout — вывод чека.
func TestShellSubprocessRun(t *testing.T) {
	if testing.Short() {
		t.Skip("shell-проба (не -short)")
	}
	r := New(Config{Mode: "subprocess", Timeout: 30 * time.Second})
	res, err := r.Run(context.Background(), "shell", sumTaskFiles(
		"#!/usr/bin/env bash\nsum=0; n=0\nwhile read -r line; do\n  for x in $line; do sum=$((sum + x)); n=$((n + 1)); done\ndone\necho \"sum=$sum\"\necho \"avg=$((sum / n))\"\n"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit_code = %d, stdout:\n%s", res.ExitCode, res.Stdout)
	}
	if len(res.Tests) != 1 || !res.Tests[0].Passed {
		t.Fatalf("tests = %+v, want [{check.sh true}]", res.Tests)
	}
	if !strings.Contains(res.Stdout, "OK") {
		t.Fatalf("stdout без вывода чека: %q", res.Stdout)
	}
}

// TestShellSubprocessFail — неверное решение: чек падает (passed=false),
// вывод чека с причиной доступен кандидату.
func TestShellSubprocessFail(t *testing.T) {
	if testing.Short() {
		t.Skip("shell-проба (не -short)")
	}
	r := New(Config{Mode: "subprocess", Timeout: 30 * time.Second})
	res, err := r.Run(context.Background(), "shell", sumTaskFiles(
		"#!/usr/bin/env bash\necho \"sum=42\"\n"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode == 0 {
		t.Fatalf("чек неверного решения прошёл: stdout:\n%s", res.Stdout)
	}
	if len(res.Tests) != 1 || res.Tests[0].Passed {
		t.Fatalf("tests = %+v, want [{check.sh false}]", res.Tests)
	}
	if !strings.Contains(res.Stdout, "FAIL") {
		t.Fatalf("stdout без причины провала: %q", res.Stdout)
	}
}

// TestShellForkBombGroupKill — fork-bomb в решении гасится group-kill'ом по
// таймауту: run завершается с timeout, дочерние sleep-процессы умирают
// (Setpgid+SIGKILL), хост не остаётся с деревом процессов.
func TestShellForkBombGroupKill(t *testing.T) {
	if testing.Short() {
		t.Skip("shell-проба (не -short)")
	}
	baseline := countSleepProcs(t)
	r := New(Config{Mode: "subprocess", Timeout: 5 * time.Second})
	start := time.Now()
	res, err := r.Run(context.Background(), "shell", map[string]string{
		"solution.sh": "#!/usr/bin/env bash\nfor i in $(seq 1 300); do sleep 30 & done\nwait\n",
		"check.sh":    "#!/usr/bin/env bash\nbash solution.sh\n",
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Timeout {
		t.Fatalf("fork-bomb не дал timeout: %+v (%v)", res, elapsed)
	}
	if elapsed > 15*time.Second {
		t.Fatalf("run завис: %v", elapsed)
	}
	time.Sleep(1500 * time.Millisecond) // время на зачистку группы
	if after := countSleepProcs(t); after > baseline {
		t.Fatalf("после group-kill осталось sleep-процессов: baseline=%d after=%d", baseline, after)
	}
}

// countSleepProcs — число sleep 30 процессов текущего uid (fork-bomb-детект).
func countSleepProcs(t *testing.T) int {
	t.Helper()
	out, err := exec.Command("bash", "-c",
		fmt.Sprintf("pgrep -f 'sleep 30' -u %d | wc -l", os.Getuid())).Output()
	if err != nil {
		t.Skipf("pgrep недоступен: %v", err)
	}
	n := 0
	_, _ = fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &n)
	return n
}

// TestShellDockerForkBomb — spec-20: бесконечная fork-bomb в решении
// гасится контейнером: --pids-limit=128 не даёт расти дереву процессов,
// таймаут раннера убивает контейнер по cidfile (docker kill), --rm зачищает
// остатки. Ключевые assertions: раннер завершился за ограниченное время,
// timeout=true, после прогона не осталось контейнеров нашего образа.
func TestShellDockerForkBomb(t *testing.T) {
	if testing.Short() {
		t.Skip("docker-проба (не -short)")
	}
	if os.Getenv("SANDBOX_DOCKER") == "" {
		t.Skip("SANDBOX_DOCKER не задан — docker-пробы опциональны")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("нет docker CLI")
	}
	if out, err := exec.Command("docker", "info").CombinedOutput(); err != nil {
		t.Skipf("docker-демон недоступен: %v (%s)", err, out)
	}
	r := New(Config{Mode: "docker", Timeout: 20 * time.Second})
	start := time.Now()
	res, err := r.Run(context.Background(), "shell", map[string]string{
		// Классическая бомба: каждая вызывавшая функция плодит двух детей.
		"solution.sh": "#!/usr/bin/env bash\nb(){ b | b & }; b\n",
		"check.sh":    "#!/usr/bin/env bash\nbash solution.sh\necho OK\n",
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Timeout {
		t.Fatalf("форк-бомба в контейнере не дала timeout: %+v (%v)", res, elapsed)
	}
	if elapsed > 40*time.Second {
		t.Fatalf("контейнер не был убит вовремя: %v", elapsed)
	}
	time.Sleep(2 * time.Second) // время на --rm и зачистку
	out, _ := exec.Command("docker", "ps", "-a", "--filter", "ancestor=debian:12-slim", "--format", "{{.ID}}").CombinedOutput()
	if n := len(strings.Split(strings.TrimSpace(string(out)), "\n")); strings.TrimSpace(string(out)) != "" && n > 0 {
		t.Fatalf("остались контейнеры debian:12-slim после --rm: %s", string(out))
	}
}

// TestShellDockerRun — prod-режим: задача в контейнере (debian:12-slim)
// проходит с тем же контрактом. Опционально (docker CLI + демон + образ).
func TestShellDockerRun(t *testing.T) {
	if testing.Short() {
		t.Skip("docker-проба (не -short)")
	}
	if os.Getenv("SANDBOX_DOCKER") == "" {
		t.Skip("SANDBOX_DOCKER не задан — docker-пробы опциональны")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("нет docker CLI")
	}
	if out, err := exec.Command("docker", "info").CombinedOutput(); err != nil {
		t.Skipf("docker-демон недоступен: %v (%s)", err, out)
	}
	r := New(Config{Mode: "docker", Timeout: 60 * time.Second})
	res, err := r.Run(context.Background(), "shell", sumTaskFiles(
		"#!/usr/bin/env bash\nsum=0; n=0\nwhile read -r line; do\n  for x in $line; do sum=$((sum + x)); n=$((n + 1)); done\ndone\necho \"sum=$sum\"\necho \"avg=$((sum / n))\"\n"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode != 0 || len(res.Tests) != 1 || !res.Tests[0].Passed {
		t.Fatalf("docker shell: %+v\nstdout:\n%s", res, res.Stdout)
	}
}
