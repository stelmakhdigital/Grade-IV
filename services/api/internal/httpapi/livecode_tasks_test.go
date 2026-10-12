package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"testing"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
	"nhooyr.io/websocket"
)

// liveCodeBankTask — задача банка для mock-sandbox.
func liveCodeBankTask(id string) map[string]any {
	return map[string]any{
		"id":        id,
		"stack":     "go",
		"grades":    []string{"middle"},
		"title":     "Задача " + id,
		"statement": "Условие " + id,
		"files":     map[string]string{"solution.go": "package task\n"},
	}
}

// sandboxTasksHandler — mock sandbox GET /api/v1/tasks?stack=&grade=.
func sandboxTasksHandler(pools map[string][]map[string]any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("stack") + "|" + r.URL.Query().Get("grade")
		w.Header().Set("Content-Type", "application/json")
		out := []map[string]any{}
		for _, t := range pools[key] {
			out = append(out, t)
		}
		_ = json.NewEncoder(w).Encode(out)
	}
}

func idsOf(list []map[string]any) []string {
	out := make([]string, 0, len(list))
	for _, t := range list {
		out = append(out, fmt.Sprint(t["id"]))
	}
	return out
}

func TestPickLiveCodeTasks(t *testing.T) {
	pool := make([]liveCodeTask, 0, 5)
	for i := 0; i < 5; i++ {
		pool = append(pool, liveCodeTask{ID: fmt.Sprintf("m%d", i), Title: fmt.Sprintf("m%d", i)})
	}
	stretch := []liveCodeTask{{ID: "s0", Title: "s0"}, {ID: "s1", Title: "s1"}}

	// middle: N=3, последняя — stretch, без повторов.
	for _, seed := range []int64{1, 2, 42} {
		rng := rand.New(rand.NewSource(seed))
		got := pickLiveCodeTasks(models.GradeMiddle, pool, stretch, rng)
		if len(got) != 3 {
			t.Fatalf("middle: len = %d, want 3", len(got))
		}
		seen := map[string]bool{}
		for i, task := range got {
			if seen[task.ID] {
				t.Fatalf("повтор: %v", idsOf2(got))
			}
			seen[task.ID] = true
			if i < 2 && task.ID[:1] != "m" {
				t.Fatalf("позиция %d не из основного пула: %s", i, task.ID)
			}
		}
		if got[2].ID != "s0" && got[2].ID != "s1" {
			t.Fatalf("последняя не из stretch-пула: %v", idsOf2(got))
		}
	}

	// junior: N=2, без stretch — только основной пул.
	rng := rand.New(rand.NewSource(1))
	got := pickLiveCodeTasks(models.GradeJunior, pool, nil, rng)
	if len(got) != 2 {
		t.Fatalf("junior: len = %d, want 2", len(got))
	}
	if got[0].ID == got[1].ID {
		t.Fatalf("junior: повторы")
	}

	// Пул меньше N — взять всё (деградация).
	small := pool[:1]
	got = pickLiveCodeTasks(models.GradeMiddle, small, nil, rand.New(rand.NewSource(1)))
	if len(got) != 1 || got[0].ID != "m0" {
		t.Fatalf("малый пул: %v", idsOf2(got))
	}

	// staff: без stretch.
	got = pickLiveCodeTasks(models.GradeStaff, pool, nil, rand.New(rand.NewSource(1)))
	if len(got) != 3 {
		t.Fatalf("staff: len = %d, want 3", len(got))
	}
}

func idsOf2(list []liveCodeTask) []string {
	out := make([]string, 0, len(list))
	for _, t := range list {
		out = append(out, t.ID)
	}
	return out
}

// TestEnterLiveCodeSelection — WS-интеграция: N задач, task=tasks[0], stretch
// (последняя из senior-пула для middle), детерминизм по LIVECODE_TASK_SEED,
// событие livecode_tasks.
func TestEnterLiveCodeSelection(t *testing.T) {
	t.Setenv("LIVECODE_TASK_SEED", "1")
	middlePool := []map[string]any{
		liveCodeBankTask("go-m1"), liveCodeBankTask("go-m2"), liveCodeBankTask("go-m3"),
		liveCodeBankTask("go-m4"), liveCodeBankTask("go-m5"),
	}
	seniorPool := []map[string]any{liveCodeBankTask("go-s1"), liveCodeBankTask("go-s2")}
	e := newRunsEnv(t, sandboxTasksHandler(map[string][]map[string]any{
		"go|middle": middlePool,
		"go|senior": seniorPool,
	}))

	enter := func() []string {
		t.Helper()
		conn := dialWS(t, e.ts, e.token, e.session)
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
		// Стартовые: stage/voice + ai_text.
		_ = wsReadTypes(t, conn, 2, 3*time.Second)
		wsWriteJSON(t, conn, map[string]any{"type": "ui", "name": "stage_action",
			"payload": map[string]string{"stage": "livecode"}})
		// stage (движок) → stage (enterLiveCode, с tasks) → ai_text (mock).
		raw := wsReadRaw(t, conn, 3, 5*time.Second)
		var stageMsg map[string]any
		for _, b := range raw {
			var m map[string]any
			if err := json.Unmarshal(b, &m); err != nil {
				continue
			}
			if m["type"] == "stage" && m["tasks"] != nil {
				stageMsg = m
			}
		}
		if stageMsg == nil {
			t.Fatalf("нет stage-сообщения с tasks: %s", string(raw[1]))
		}
		tasks, _ := stageMsg["tasks"].([]any)
		if len(tasks) != 3 {
			t.Fatalf("tasks: %d, want 3 (middle)", len(tasks))
		}
		// task = tasks[0] (совместимость).
		task, _ := stageMsg["task"].(map[string]any)
		first, _ := tasks[0].(map[string]any)
		if task == nil || task["id"] != first["id"] {
			t.Fatalf("task != tasks[0]: %v vs %v", task, first)
		}
		ids := make([]string, 0, 3)
		for i, item := range tasks {
			tm, _ := item.(map[string]any)
			id := fmt.Sprint(tm["id"])
			ids = append(ids, id)
			if i < 2 && !(id >= "go-m1" && id <= "go-m5") {
				t.Fatalf("позиция %d не из middle-пула: %s", i, id)
			}
		}
		// Stretch: последняя — из senior-пула (spec-13B T1.2).
		if ids[2] != "go-s1" && ids[2] != "go-s2" {
			t.Fatalf("последняя не из senior-пула: %v", ids)
		}
		return ids
	}

	first := enter()
	// Событие livecode_tasks сохранено (для отчёта).
	events, err := e.server.sessions.ListEvents(context.Background(), e.session)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	found := false
	for _, ev := range events {
		if ev.Kind == "livecode_tasks" {
			found = true
		}
	}
	if !found {
		t.Fatalf("нет события livecode_tasks: %+v", events)
	}

	// Детерминизм: та же seed → тот же выбор (новая сессия).
	e2session, _ := createSession(t, e.ts, e.token, "middle", "go")
	_ = e2session
	e.session = e2session
	second := enter()
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("seed=1: разные списки: %v vs %v", first, second)
		}
	}
}
