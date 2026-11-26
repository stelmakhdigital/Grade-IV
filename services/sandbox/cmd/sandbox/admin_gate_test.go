package main

// Gate генерации не должен умирать вместе с запросом клиента: api ждёт конечное
// время и отваливается по таймауту, а работа надопускается и сохраняется.
import (
	"context"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/stelmakhdigital/grade-iv/services/sandbox/internal/runner"
	"github.com/stelmakhdigital/grade-iv/services/sandbox/internal/tasks"
)

// ctxRunner запоминает состояние контекста В МОМЕНТ прогона: проверять после
// возврата gateAndStore нельзя — его defer cancel() к тому времени уже отработает.
type ctxRunner struct {
	ctxErr      error
	hasDeadline bool
}

func (r *ctxRunner) Run(ctx context.Context, _ string, _, _ map[string]string) (runner.Result, error) {
	r.ctxErr = ctx.Err()
	_, r.hasDeadline = ctx.Deadline()
	return runner.Result{Passed: true, ExitCode: 0}, nil
}

func TestGateSurvivesClientCancel(t *testing.T) {
	bank, err := tasks.Load(filepath.Join(t.TempDir(), "gen.json"))
	if err != nil {
		t.Fatal(err)
	}
	rn := &ctxRunner{}
	task := &tasks.Task{
		ID:        "keep-me",
		Stack:     "shell",
		Grades:    []string{"junior"},
		Title:     "Тест",
		Statement: "Написать решение, которое печатает ровно hello.",
		Files: map[string]string{
			"solution.sh": "#!/bin/sh\n",
			"check.sh":    "#!/bin/sh\nbash solution.sh | grep -q hello\n",
		},
		Meta: &tasks.Meta{Family: "shell-stats", Seed: 1},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // клиент ушёл до того, как gate закончил

	_, status, err := gateAndStore(ctx, bank, rn, task,
		map[string]string{"solution.sh": "#!/bin/sh\necho hello\n"}, nil,
		slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("ошибка gate: %v", err)
	}
	if status != http.StatusCreated {
		t.Fatalf("status = %d, хотим 201", status)
	}
	if rn.ctxErr != nil {
		t.Errorf("контекст gate отменён вместе с клиентом: %v", rn.ctxErr)
	}
	if _, ok := bank.Get(task.ID); !ok {
		t.Error("задача не попала в банк")
	}
	// Потолок gateTimeout остаётся: без него зависший прогон висел бы вечно.
	if !rn.hasDeadline {
		t.Error("у контекста gate нет предельного времени")
	}
}
