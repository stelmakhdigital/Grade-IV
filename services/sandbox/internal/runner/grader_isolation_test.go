package runner

// BUG-20261122-033 — файлы градатора (файлы банка) лежали в том же workdir,
// где исполняется код кандидата. Отсюда две дыры, закрытые этим регрессом:
//   - чтение: `cat check.sh >&2` в решении возвращало исходник градатора и
//     ожидаемые значения кандидату (stdout прогона идёт кандидату целиком) и
//     давал гарантированный pass — ассерты чека читались из того же каталога;
//   - запись: решение переписывало чек (`echo 'exit 0' > check.sh`) до того,
//     как до его строк доходило выполнение, и проходило чек.
//
// Файлы банка пишутся в соседний каталог (вне cwd решения): в docker он
// монтируется в /grade только на чтение, в subprocess остаётся соседним
// каталогом с правами 0444. Read/write-изоляция по абсолютному пути достижима
// только разделением uid (код кандидата идёт под uid sandbox) — см. комментарий
// к Run; здесь проверяется то, что закрыто: cwd решения, дерево решения и
// монтирование /grade на запись.

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/sandbox/internal/tasks"
)

// runModes — оба режима прогона; docker-подтесты идут только при SANDBOX_DOCKER=1
// (конвенция TestSandboxDockerProbe).
var runModes = []string{"subprocess", "docker"}

// runnerFor — раннер режима mode (docker — с проверкой CLI и демона).
// commands — подмена команд прогона (nil — дефолт).
func runnerFor(t *testing.T, mode string, timeout time.Duration, commands map[string]string) *Runner {
	t.Helper()
	if testing.Short() {
		t.Skip("проба запуска кода (не -short)")
	}
	if mode == "subprocess" {
		return New(Config{Mode: mode, Timeout: timeout, Commands: commands})
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
	return New(Config{Mode: "docker", Timeout: timeout, Images: envImages(), Commands: commands})
}

// splitBankTask — файлы задачи банка так же, как в handleRun: файлы кандидата
// и файлы градатора раздельно (BUG-20261122-033).
func splitBankTask(t *testing.T, id string) (files, trusted map[string]string) {
	t.Helper()
	bank, err := tasks.Default()
	if err != nil {
		t.Fatalf("банк задач: %v", err)
	}
	task, ok := bank.Get(id)
	if !ok {
		t.Fatalf("задача %s не найдена в банке", id)
	}
	files, trusted = map[string]string{}, map[string]string{}
	for name, content := range task.Files {
		if tasks.IsGraderFile(name, task.Stack) {
			trusted[name] = content
		} else {
			files[name] = content
		}
	}
	if len(trusted) == 0 {
		t.Fatalf("у задачи %s нет файлов градатора в банке", id)
	}
	return files, trusted
}

// TestGraderNotInCandidateWorkdir — код кандидата не видит файлы банка ни в
// cwd, ни в дереве своего каталога (ни одного из стеков, в обоих режимах).
// Команда прогона — листинг cwd: тест не зависит от наличия go/pytest в окружении.
func TestGraderNotInCandidateWorkdir(t *testing.T) {
	probe := "pwd; ls -aR ."
	listing := map[string]string{"go": probe, "python": probe, "shell": probe}
	tasksByStack := map[string]string{"go": "go-reverse-string", "python": "py-palindrome", "shell": "shell-sum-avg"}
	for _, stack := range []string{"go", "python", "shell"} {
		_, trusted := splitBankTask(t, tasksByStack[stack])
		for _, mode := range runModes {
			t.Run(stack+"/"+mode, func(t *testing.T) {
				rn := runnerFor(t, mode, 60*time.Second, listing)
				res, err := rn.Run(context.Background(), stack, map[string]string{"solution.txt": "x"}, trusted)
				if err != nil {
					t.Fatalf("Run: %v", err)
				}
				for name := range trusted {
					if strings.Contains(res.Stdout, name) {
						t.Fatalf("файл банка %s виден коду кандидата:\n%s", name, res.Stdout)
					}
				}
				if strings.Contains(res.Stdout, "overlay.json") {
					t.Fatalf("оверлей go виден в каталоге кандидата:\n%s", res.Stdout)
				}
			})
		}
	}
}

// TestShellGraderCheatCannotPass — сдача из issue: решение читает чек и печатает
// то, что тот ассертит. Градатор должен быть для него недоступен — иначе
// «скрытый градатор» формальность, а pass гарантирован ожидаемыми значениями.
//
// Чек читается в stderr: stdout решения чек съедает в OUT=$(...), а stderr
// уходит в общий вывод прогона и возвращается кандидату (runs_handlers.go).
func TestShellGraderCheatCannotPass(t *testing.T) {
	cheat := "#!/usr/bin/env bash\ncat check.sh >&2\n" +
		"grep -oE 'sum=[0-9]+|avg=[0-9]+' check.sh | sed 's/^/READ /' >&2\n"
	_, trusted := splitBankTask(t, "shell-sum-avg")
	for _, mode := range runModes {
		t.Run(mode, func(t *testing.T) {
			rn := runnerFor(t, mode, 60*time.Second, nil)
			res, err := rn.Run(context.Background(), "shell", map[string]string{"solution.sh": cheat}, trusted)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if strings.Contains(res.Stdout, "READ sum=10") || strings.Contains(res.Stdout, "READ avg=2") {
				t.Fatalf("код кандидата прочитал градатор и украл ожидаемые значения:\n%s", res.Stdout)
			}
			for _, leak := range []string{"fail()", "grep -qx", "решение завершилось с ошибкой"} {
				if strings.Contains(res.Stdout, leak) {
					t.Fatalf("в выводе прогона текст градатора (%s):\n%s", leak, res.Stdout)
				}
			}
			if res.Passed {
				t.Fatalf("сдача без решения (только чтение градатора) прошла чек:\n%s", res.Stdout)
			}
		})
	}
}

// TestShellGraderNotWritable — второй шаг из issue: решение переписывает чек до
// выполнения его последних строк. В cwd решения чека быть не должно, а /grade
// в docker примонтирован на чтение (проверяем unlink — правами 0444 он не
// блокируется, только монтированием).
func TestShellGraderNotWritable(t *testing.T) {
	tamper := "#!/usr/bin/env bash\n" +
		"for f in check.sh ../grade/check.sh; do\n" +
		"  [ -e \"$f\" ] && echo 'exit 0' > \"$f\" 2>/dev/null && echo TAMPERED $f\n" +
		"done\n" +
		"if [ -e /grade/check.sh ]; then rm -f /grade/check.sh 2>/dev/null && echo REMOVED; fi\n" +
		"echo 'sum=0'\n"
	_, trusted := splitBankTask(t, "shell-sum-avg")
	for _, mode := range runModes {
		t.Run(mode, func(t *testing.T) {
			rn := runnerFor(t, mode, 60*time.Second, nil)
			res, err := rn.Run(context.Background(), "shell", map[string]string{"solution.sh": tamper}, trusted)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			for _, marker := range []string{"TAMPERED", "REMOVED"} {
				if strings.Contains(res.Stdout, marker) {
					t.Fatalf("градатор доступен коду кандидата на запись/удаление (%s):\n%s", marker, res.Stdout)
				}
			}
			if res.Passed {
				t.Fatalf("неверное решение прошло чек (подмена удалась?):\n%s", res.Stdout)
			}
		})
	}
}

// TestShellDecoyCheckIgnored — подставной check.sh в файлах кандидата не
// исполняется: градатор берётся из банка, а не из каталога решения.
func TestShellDecoyCheckIgnored(t *testing.T) {
	if testing.Short() {
		t.Skip("shell-проба (не -short)")
	}
	rn := New(Config{Mode: "subprocess", Timeout: 30 * time.Second})
	res, err := rn.Run(context.Background(), "shell", map[string]string{
		"solution.sh": "#!/usr/bin/env bash\nsum=0; n=0\nwhile read -r line; do\n  for x in $line; do sum=$((sum + x)); n=$((n + 1)); done\ndone\necho \"sum=$sum\"\necho \"avg=$((sum / n))\"\n",
		"check.sh":    "#!/usr/bin/env bash\necho DECOY\nexit 0\n",
	}, map[string]string{"check.sh": "#!/usr/bin/env bash\nOUT=$(printf '1 2 3\\n4\\n' | bash solution.sh) || exit 1\necho \"$OUT\" | grep -qx 'sum=10' || exit 1\necho BANK-OK\n"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(res.Stdout, "DECOY") {
		t.Fatalf("исполнен check.sh кандидата, а не банка:\n%s", res.Stdout)
	}
	if !strings.Contains(res.Stdout, "BANK-OK") || !res.Passed {
		t.Fatalf("честное решение не прошло чек банка:\n%s", res.Stdout)
	}
}

// TestGoGraderTestsOverlaid — честное go-решение проходит тестами банка, которых
// нет в каталоге кандидата (подмешаны `go test -overlay`), а «троян» с тем же
// именем не исполняется и не виден в дереве решения.
func TestGoGraderTestsOverlaid(t *testing.T) {
	if testing.Short() {
		t.Skip("go-проба (не -short)")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("нет go в PATH (subprocess-режим не может собрать пакет)")
	}
	files, trusted := splitBankTask(t, "go-reverse-string")
	files["solution.go"] = "package task\n\nfunc Reverse(s string) string {\n\tr := []rune(s)\n\tfor i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {\n\t\tr[i], r[j] = r[j], r[i]\n\t}\n\treturn string(r)\n}\n"
	// Троян поверх имени теста банка: исполняться не должен.
	files["solution_test.go"] = "package task\n\nimport \"testing\"\n\nfunc TestTrojan(t *testing.T) { t.Log(\"PWNED\") }\n"
	rn := New(Config{Mode: "subprocess", Timeout: 180 * time.Second})
	res, err := rn.Run(context.Background(), "go", files, trusted)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Passed {
		t.Fatalf("честное решение не прошло тестами банка:\n%s\n%s", res.Stdout, res.Stderr)
	}
	for _, tr := range res.Tests {
		if strings.Contains(tr.Name, "TestTrojan") {
			t.Fatalf("исполнен тест-троян кандидата: %+v", res.Tests)
		}
	}
	if strings.Contains(res.Stdout, "PWNED") {
		t.Fatalf("вывод теста-трояна в прогоне:\n%s", res.Stdout)
	}
}

// TestPythonGraderTestsOutsideWorkdir — pytest берёт тесты из каталога градатора
// (cwd — решение), честное решение проходит, а print(open("solution_test.py"))
// ничего не печатает: теста банка в каталоге решения нет.
func TestPythonGraderTestsOutsideWorkdir(t *testing.T) {
	if testing.Short() {
		t.Skip("python-проба (не -short)")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("нет python3")
	}
	_, trusted := splitBankTask(t, "py-palindrome")
	rn := New(Config{Mode: "subprocess", Timeout: 60 * time.Second})

	res, err := rn.Run(context.Background(), "python",
		map[string]string{"solution.py": "def is_palindrome(s: str) -> bool:\n    t = [c.lower() for c in s if c.isalnum()]\n    return t == t[::-1]\n"}, trusted)
	if strings.Contains(res.Stdout, "No module named pytest") {
		t.Skip("нет pytest в окружении (prod-образ grade/python-tests — проба под SANDBOX_DOCKER=1)")
	}
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Passed {
		t.Fatalf("честное решение не прошло тестами банка:\n%s\n%s", res.Stdout, res.Stderr)
	}

	snoop := "print(open('solution_test.py').read())\n"
	res, err = rn.Run(context.Background(), "python", map[string]string{"solution.py": snoop}, trusted)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(res.Stdout, "import ") || strings.Contains(res.Stdout, "def test_") {
		t.Fatalf("код кандидата прочитал тесты банка:\n%s", res.Stdout)
	}
	if res.Passed {
		t.Fatalf("решение-шпион прошло тесты банка:\n%s", res.Stdout)
	}
}
