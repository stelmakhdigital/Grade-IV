package runner

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeCmd — подменяет команды на детерминированные (не зависит от go/pytest в CI).
func fakeCmds(goCmd, pyCmd string) map[string]string {
	return map[string]string{"go": goCmd, "python": pyCmd}
}

func TestRunSubprocessPass(t *testing.T) {
	rn := New(Config{Mode: "subprocess", Commands: fakeCmds("printf 'hello\\n' && exit 0", "exit 0")})
	res, err := rn.Run(context.Background(), "go", map[string]string{"a.txt": "x"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.ExitCode != 0 || !res.Passed || res.Timeout {
		t.Fatalf("result: %+v", res)
	}
	if res.Stdout != "hello\n" {
		t.Fatalf("stdout: %q", res.Stdout)
	}
	if res.DurationMS < 0 {
		t.Fatalf("duration: %d", res.DurationMS)
	}
}

func TestRunSubprocessFail(t *testing.T) {
	rn := New(Config{Mode: "subprocess", Commands: fakeCmds("exit 5", "exit 7")})
	for stack, want := range map[string]int{"go": 5, "python": 7} {
		res, err := rn.Run(context.Background(), stack, map[string]string{"a.txt": "x"})
		if err != nil {
			t.Fatalf("%s: %v", stack, err)
		}
		if res.ExitCode != want || res.Passed {
			t.Fatalf("%s: %+v", stack, res)
		}
		if stack == "python" {
			if len(res.Tests) != 1 || res.Tests[0].Passed {
				t.Fatalf("python tests: %+v", res.Tests)
			}
		}
	}
}

func TestRunTimeout(t *testing.T) {
	rn := New(Config{
		Mode:    "subprocess",
		Timeout: 300 * time.Millisecond,
		Commands: fakeCmds(
			"sh -c 'sleep 5; echo never'", // вложенный sleep — проверка убийства группы
			"sh -c 'sleep 5'"),
	})
	res, err := rn.Run(context.Background(), "go", map[string]string{"a.txt": "x"})
	if err != nil {
		t.Fatalf("timeout должен давать результат, а не ошибку: %v", err)
	}
	if !res.Timeout || res.ExitCode != 124 || res.Passed {
		t.Fatalf("result: %+v", res)
	}
	if !strings.Contains(res.Stderr, "[timeout]") {
		t.Fatalf("stderr: %q", res.Stderr)
	}
}

func TestRunBadStack(t *testing.T) {
	rn := New(Config{Mode: "subprocess"})
	if _, err := rn.Run(context.Background(), "rust", map[string]string{"a": "x"}); !errors.Is(err, ErrUnsupportedStack) {
		t.Fatalf("ожидался ErrUnsupportedStack: %v", err)
	}
}

func TestRunBadFiles(t *testing.T) {
	rn := New(Config{Mode: "subprocess"})
	bad := map[string]map[string]string{
		"empty":  {},
		"dotdot": {"../evil.sh": "#!/bin/sh\necho pwned"},
		"abs":    {"/etc/passwd": "x"},
	}
	for name, files := range bad {
		if _, err := rn.Run(context.Background(), "go", files); !errors.Is(err, ErrBadFiles) {
			t.Fatalf("%s: ожидался ErrBadFiles, получено %v", name, err)
		}
	}
}

func TestSafePath(t *testing.T) {
	ok := []string{"main.go", "tests/test.go", "a/b/c.txt"}
	for _, p := range ok {
		if !safePath(p) {
			t.Errorf("%q: ожидалось допустимо", p)
		}
	}
	bad := []string{"", "../x", "/x", "a/../../b", ".."}
	for _, p := range bad {
		if safePath(p) {
			t.Errorf("%q: ожидалось недопустимо", p)
		}
	}
}

func TestParseGoTests(t *testing.T) {
	raw := []byte(`
{"Action":"run","Test":"TestA"}
{"Action":"output","Test":"TestA","Output":"..."}
{"Action":"pass","Test":"TestA"}
{"Action":"run","Test":"TestB"}
{"Action":"fail","Test":"TestB"}
{"Action":"run","Test":"TestC"}
{"Action":"skip","Test":"TestC"}
`)
	tests := parseGoTests(raw, nil)
	byName := map[string]bool{}
	for _, tr := range tests {
		byName[tr.Name] = tr.Passed
	}
	if !byName["TestA"] || byName["TestB"] || !byName["TestC"] {
		t.Fatalf("parsed: %+v", tests)
	}
	if len(tests) != 3 {
		t.Fatalf("len: %d", len(tests))
	}
	// Пустой ввод → fallback.
	fb := []TestResult{{Name: "all", Passed: true}}
	if got := parseGoTests([]byte("nothing"), fb); len(got) != 1 || got[0].Name != "all" {
		t.Fatalf("fallback: %+v", got)
	}
}

func TestFilesWrittenToWorkdir(t *testing.T) {
	// Команда проверяет наличие вложенного файла — путь создаётся рекурсивно.
	rn := New(Config{Mode: "subprocess", Commands: fakeCmds("test -f sub/dir/a.txt && echo ok", "test -f sub/dir/a.txt && echo ok")})
	res, err := rn.Run(context.Background(), "go", map[string]string{"sub/dir/a.txt": "data"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Stdout != "ok\n" || !res.Passed {
		t.Fatalf("result: %+v", res)
	}
}
