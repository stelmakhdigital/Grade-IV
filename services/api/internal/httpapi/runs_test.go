package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/config"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/llm"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

// runsEnv — окружение с прямым доступом к движку (для перехода в Live-Code) и mock-sandbox.
type runsEnv struct {
	ts      *httptest.Server
	server  *Server
	token   string
	session int64
}

func newRunsEnv(t *testing.T, sandboxHandler http.HandlerFunc) *runsEnv {
	t.Helper()
	sandbox := httptest.NewServer(sandboxHandler)
	t.Cleanup(sandbox.Close)

	cfg := &config.Config{
		Addr:           ":0",
		DatabaseURL:    "sqlite://:memory:",
		JWTSecret:      "test-secret",
		JWTExpiryHours: 1,
		SandboxURL:     sandbox.URL,
	}
	database, dialect, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.Migrate(context.Background(), database, dialect); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	logger := discardLogger()
	srv := NewWithLLM(cfg, database, dialect, logger, llm.NewMockProvider())
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	e := &runsEnv{ts: ts, server: srv}

	// Регистрация и сессия.
	code, m := doJSON(t, "POST", ts.URL+"/api/v1/auth/register",
		map[string]string{"email": "runs@example.com", "password": "password1"}, nil)
	if code != http.StatusCreated {
		t.Fatalf("register: %d", code)
	}
	e.token, _ = m["token"].(string)
	sid, _ := createSession(t, ts, e.token, "middle", "go")
	e.session = sid
	return e
}

// toLiveCode переводит сессию в Live-Code через движок (WS-аналог stage_action).
func (e *runsEnv) toLiveCode(t *testing.T) {
	t.Helper()
	if _, err := e.server.Engine().Transition(e.session, models.StageLiveCode); err != nil {
		t.Fatalf("transition livecode: %v", err)
	}
}

// assignTasks — фиксирует событие livecode_tasks: задачи, «назначенные» сессии
// (то же, что делает enterLiveCode при входе на стадию).
func (e *runsEnv) assignTasks(t *testing.T, ids ...string) {
	t.Helper()
	tasks := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		tasks = append(tasks, map[string]any{"id": id, "title": "Задача " + id})
	}
	if _, err := e.server.eventData(context.Background(), e.session, "livecode_tasks",
		map[string]any{"tasks": tasks}); err != nil {
		t.Fatalf("livecode_tasks: %v", err)
	}
}

func TestRunsProxy(t *testing.T) {
	mockResult := map[string]any{
		"exit_code": 0, "stdout": "ok", "stderr": "", "duration_ms": 42,
		"passed": true, "tests": []map[string]any{{"name": "TestX", "passed": true}},
	}
	e := newRunsEnv(t, func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["stack"] != "go" {
			t.Errorf("stack = %v, want go", req["stack"])
		}
		if req["action"] != "test" {
			t.Errorf("action = %v", req["action"])
		}
		if files, ok := req["files"].(map[string]any); !ok || files["main.go"] == nil {
			t.Errorf("files = %v", req["files"])
		}
		writeMock(w, http.StatusOK, mockResult)
	})
	e.toLiveCode(t)
	e.assignTasks(t, "go-two-sum")

	code, m := doJSON(t, "POST", e.ts.URL+fmt.Sprintf("/api/v1/sessions/%d/runs", e.session),
		map[string]any{"files": map[string]string{"main.go": "package main"}, "task_id": "go-two-sum"},
		map[string]string{"Authorization": "Bearer " + e.token})
	if code != http.StatusOK {
		t.Fatalf("runs: %d (%v)", code, m)
	}
	if m["passed"] != true || m["exit_code"] != float64(0) {
		t.Fatalf("result: %v", m)
	}

	// Сдача сохранена в БД + событие code_run.
	subs, err := e.server.submissions.ListBySession(context.Background(), e.session)
	if err != nil {
		t.Fatalf("submissions: %v", err)
	}
	if len(subs) != 1 || subs[0].TaskID != "go-two-sum" || subs[0].Stdout != "ok" {
		t.Fatalf("submission: %+v", subs)
	}
	events, _ := e.server.sessions.ListEvents(context.Background(), e.session)
	found := false
	for _, ev := range events {
		if ev.Kind == "code_run" {
			found = true
		}
	}
	if !found {
		t.Fatalf("нет события code_run: %+v", events)
	}
}

func TestRunsStageGate(t *testing.T) {
	e := newRunsEnv(t, func(w http.ResponseWriter, _ *http.Request) {
		writeMock(w, http.StatusOK, map[string]any{"passed": true})
	})
	// Без перехода в Live-Code → 409.
	code, _ := doJSON(t, "POST", e.ts.URL+fmt.Sprintf("/api/v1/sessions/%d/runs", e.session),
		map[string]any{"files": map[string]string{"main.go": "x"}},
		map[string]string{"Authorization": "Bearer " + e.token})
	if code != http.StatusConflict {
		t.Fatalf("voice: %d, want 409", code)
	}

	// Пустые файлы → 400.
	e.toLiveCode(t)
	code, _ = doJSON(t, "POST", e.ts.URL+fmt.Sprintf("/api/v1/sessions/%d/runs", e.session),
		map[string]any{"files": map[string]string{}},
		map[string]string{"Authorization": "Bearer " + e.token})
	if code != http.StatusBadRequest {
		t.Fatalf("empty files: %d, want 400", code)
	}
}

func TestRunsSandboxDown(t *testing.T) {
	e := newRunsEnv(t, func(w http.ResponseWriter, _ *http.Request) {
		writeMock(w, http.StatusServiceUnavailable, map[string]string{
			"code": "sandbox_unavailable", "message": "docker недоступен",
		})
	})
	e.toLiveCode(t)
	e.assignTasks(t, "go-two-sum")
	code, m := doJSON(t, "POST", e.ts.URL+fmt.Sprintf("/api/v1/sessions/%d/runs", e.session),
		map[string]any{"files": map[string]string{"main.go": "x"}, "task_id": "go-two-sum"},
		map[string]string{"Authorization": "Bearer " + e.token})
	if code != http.StatusServiceUnavailable {
		t.Fatalf("sandbox 503: %d (%v), want 503", code, m)
	}
}

// TestRunsTaskIDGate — issue #8: task_id обязателен и должен быть среди задач,
// назначенных сессии. Без task_id sandbox не подставляет тесты банка, и stub
// кандидата «проходит» (go test по пакету без тестов = exit 0); такой прогон
// не должен дойти ни до sandbox, ни до submissions, ни до события code_run.
func TestRunsTaskIDGate(t *testing.T) {
	var sandboxHits int32
	e := newRunsEnv(t, func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&sandboxHits, 1)
		writeMock(w, http.StatusOK, map[string]any{"passed": true, "exit_code": 0, "tests": []any{}})
	})
	e.toLiveCode(t)
	e.assignTasks(t, "go-a")
	runsURL := e.ts.URL + fmt.Sprintf("/api/v1/sessions/%d/runs", e.session)
	auth := map[string]string{"Authorization": "Bearer " + e.token}

	// Пустой task_id → 400 (то же тело, что в issue: stub без тестов банка).
	code, _ := doJSON(t, "POST", runsURL,
		map[string]any{"files": map[string]string{"solution.go": "package main\n"},
			"action": "test", "task_id": ""}, auth)
	if code != http.StatusBadRequest {
		t.Fatalf("пустой task_id: %d, want 400", code)
	}
	// task_id задачи, не назначенной этой сессии → 403.
	code, _ = doJSON(t, "POST", runsURL,
		map[string]any{"files": map[string]string{"solution.go": "package main\n"},
			"action": "test", "task_id": "go-other-grade"}, auth)
	if code != http.StatusForbidden {
		t.Fatalf("чужой task_id: %d, want 403", code)
	}

	if n := atomic.LoadInt32(&sandboxHits); n != 0 {
		t.Fatalf("отклонённые прогоны дошли до sandbox: %d", n)
	}
	subs, err := e.server.submissions.ListBySession(context.Background(), e.session)
	if err != nil {
		t.Fatalf("submissions: %v", err)
	}
	if len(subs) != 0 {
		t.Fatalf("отклонённые прогоны сохранены в submissions: %+v", subs)
	}
	events, _ := e.server.sessions.ListEvents(context.Background(), e.session)
	for _, ev := range events {
		if ev.Kind == "code_run" {
			t.Fatalf("событие code_run ушло в отчёт по отклонённому прогону: %s", ev.Data)
		}
	}

	// Назначенный task_id — прогон проходит.
	code, m := doJSON(t, "POST", runsURL,
		map[string]any{"files": map[string]string{"solution.go": "package main\n"},
			"action": "test", "task_id": "go-a"}, auth)
	if code != http.StatusOK {
		t.Fatalf("назначенный task_id: %d (%v), want 200", code, m)
	}
	if n := atomic.LoadInt32(&sandboxHits); n != 1 {
		t.Fatalf("запросов в sandbox: %d, want 1", n)
	}
}

func writeMock(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// TestRunsThrottle: прогоны сериализованы семафором в песочнице, поэтому
// кандидат, крутящий «Запустить тесты» подряд, занимает очередь у всех
// остальных (тёплый go test 1.4 с, холодный 43 с). Сессия выше лимита получает
// 429, а соседняя сессия того же пользователя — нет (ключ — сессия).
func TestRunsThrottle(t *testing.T) {
	e := newRunsEnv(t, func(w http.ResponseWriter, r *http.Request) {
		writeMock(w, http.StatusOK, map[string]any{
			"exit_code": 0, "stdout": "ok", "stderr": "", "duration_ms": 1,
			"passed": true, "tests": []map[string]any{},
		})
	})
	e.toLiveCode(t)
	e.assignTasks(t, "go-two-sum")
	u := fmt.Sprintf("%s/api/v1/sessions/%d/runs", e.ts.URL, e.session)
	h := map[string]string{"Authorization": "Bearer " + e.token}
	body := map[string]any{"files": map[string]string{"main.go": "package main"}, "task_id": "go-two-sum"}

	for i := 0; i < 6; i++ {
		if code, m := doJSON(t, "POST", u, body, h); code != http.StatusOK {
			t.Fatalf("прогон %d: %d (%v)", i+1, code, m)
		}
	}
	code, m := doJSON(t, "POST", u, body, h)
	if code != http.StatusTooManyRequests {
		t.Fatalf("седьмой прогон: %d (%v), хочу 429", code, m)
	}
}
