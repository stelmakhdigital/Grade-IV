package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// TestReportLiveCodeTasks — per-task секция отчёта (spec-13B T3):
// задачи из livecode_tasks + результат по последней сдаче (code_run).
func TestReportLiveCodeTasks(t *testing.T) {
	e := newRunsEnv(t, sandboxTasksHandler(map[string][]map[string]any{}))
	e.toLiveCode(t)
	ctx := context.Background()

	// События: 3 задачи, сдачи по двум (passed/failed), по третьей — нет.
	_, _ = e.server.eventData(ctx, e.session, "livecode_tasks", map[string]any{
		"tasks": []map[string]any{
			{"id": "go-a", "title": "А"},
			{"id": "go-b", "title": "Б"},
			{"id": "go-c", "title": "В"},
		},
	})
	_, _ = e.server.eventData(ctx, e.session, "code_run",
		map[string]any{"task_id": "go-a", "passed": false, "exit_code": 1})
	_, _ = e.server.eventData(ctx, e.session, "code_run",
		map[string]any{"task_id": "go-a", "passed": true, "exit_code": 0}) // последняя побеждает
	_, _ = e.server.eventData(ctx, e.session, "code_run",
		map[string]any{"task_id": "go-b", "passed": false, "exit_code": 1})

	if _, err := e.server.Engine().Finish(e.session); err != nil {
		t.Fatalf("finish: %v", err)
	}
	e.server.startReportGeneration(e.session)

	var body map[string]any
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		code, m := doJSON(t, "GET",
			fmt.Sprintf("%s/api/v1/sessions/%d/report", e.ts.URL, e.session), nil,
			map[string]string{"Authorization": "Bearer " + e.token})
		if code == http.StatusOK {
			body = m
			break
		}
		if code != http.StatusAccepted {
			t.Fatalf("report: %d (%v)", code, m)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if body == nil {
		t.Fatalf("отчёт не готов за 15 с")
	}
	raw, _ := json.Marshal(body["livecode_tasks"])
	var got []map[string]any
	if err := json.Unmarshal(raw, &got); err != nil || len(got) != 3 {
		t.Fatalf("livecode_tasks: %s", raw)
	}
	want := map[string]string{"go-a": "passed", "go-b": "failed", "go-c": "no_submission"}
	for _, g := range got {
		id := fmt.Sprint(g["id"])
		if g["status"] != want[id] {
			t.Errorf("задача %s: status = %v, want %s", id, g["status"], want[id])
		}
		if g["title"] == nil || fmt.Sprint(g["title"]) == "" {
			t.Errorf("задача %s: пустое название", id)
		}
	}
}
