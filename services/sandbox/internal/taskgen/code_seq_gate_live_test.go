package taskgen

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/sandbox/internal/runner"
	"github.com/stelmakhdigital/grade-iv/services/sandbox/internal/tasks"
)

// TestCodeSeqLiveGate — единственный способ убедиться, что семья code-seq
// выдаёт задачи, которые действительно различают решения: гоняем реальный gate
// на реальном песочнике (subprocess). Нужны go-тулчейн и pytest; в CI их нет в
// этой джобе, поэтому по умолчанию пропуск.
//
//	CODE_SEQ_LIVE_GATE=1 [PYTEST_PYTHON=/путь/к/python] go test ./internal/taskgen/ -run Live -v
//
// PYTEST_PYTHON — интерпретатор, у которого есть pytest (в dev он не у
// системного python3).
func TestCodeSeqLiveGate(t *testing.T) {
	if os.Getenv("CODE_SEQ_LIVE_GATE") != "1" {
		t.Skip("CODE_SEQ_LIVE_GATE=1 не задан")
	}
	py := os.Getenv("PYTEST_PYTHON")
	if py == "" {
		py = "python3"
	}
	r := runner.New(runner.Config{
		Mode:        "subprocess",
		WorkDirBase: t.TempDir(),
		Commands: map[string]string{
			"go":     "go test -count=1 -json -overlay %GRADER%/overlay.json ./...",
			"python": py + " -m pytest -q -p no:cacheprovider %GRADER%",
		},
	})
	seeds := []int64{1, 2}
	if extra := os.Getenv("GATE_SEEDS"); extra != "" {
		seeds = nil
		for _, x := range strings.Split(extra, ",") {
			n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
			if err != nil {
				t.Fatalf("GATE_SEEDS: %v", err)
			}
			seeds = append(seeds, n)
		}
	}
	for _, fam := range []string{"code-seq-go", "code-seq-py"} {
		stack, _ := StackOf(fam)
		for _, grade := range []string{"junior", "middle", "senior"} {
			for _, seed := range seeds {
				d, err := Build(fam, seed, grade)
				if err != nil {
					t.Fatalf("%s/%s/%d: build: %v", fam, grade, seed, err)
				}
				task := &tasks.Task{
					ID:        fmt.Sprintf("%s-%s-%d", fam, grade, seed),
					Stack:     stack,
					Grades:    d.Grades,
					Title:     d.Title,
					Statement: d.Statement,
					Files:     d.Files,
				}
				started := time.Now()
				ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
				v, err := Check(ctx, r, Request{Task: task, Reference: d.Reference, Mutants: d.Mutants})
				cancel()
				t.Logf("%s/%s/%d: ok=%v за %v (мутантов %d)", fam, grade, seed, v != nil && v.OK,
					time.Since(started).Round(time.Second), len(d.Mutants))
				if err != nil {
					t.Fatalf("%s/%s/%d: gate: %v", fam, grade, seed, err)
				}
				if !v.OK {
					t.Errorf("%s/%s/%d: gate отверг задачу: %s | ref=%s | mutants=%s",
						fam, grade, seed, strings.Join(v.Reasons, "; "), evidenceLine(v.Reference), evidenceLines(v.Mutants))
				}
				if v.OK && len(v.Mutants) < 3 {
					t.Errorf("%s/%s/%d: мутантов %d, ожидалось >= 3", fam, grade, seed, len(v.Mutants))
				}
			}
		}
	}
}

func evidenceLine(e *Evidence) string {
	if e == nil {
		return "нет прогона"
	}
	return fmt.Sprintf("passed=%v exit=%d %s", e.Passed, e.ExitCode, snip(e.Snippet))
}

func evidenceLines(es []Evidence) string {
	parts := make([]string, 0, len(es))
	for _, e := range es {
		parts = append(parts, fmt.Sprintf("%s:passed=%v%s", e.Name, e.Passed, snip(e.Snippet)))
	}
	return strings.Join(parts, " || ")
}

func snip(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	if len(s) > 300 {
		s = s[:300]
	}
	if s == "" {
		return ""
	}
	return " [" + s + "]"
}
