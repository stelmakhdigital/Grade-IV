package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/config"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
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
		MinutesFreeS:   3600,
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
	srv := New(cfg, database, dialect, logger)
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
	code, m := doJSON(t, "POST", e.ts.URL+fmt.Sprintf("/api/v1/sessions/%d/runs", e.session),
		map[string]any{"files": map[string]string{"main.go": "x"}},
		map[string]string{"Authorization": "Bearer " + e.token})
	if code != http.StatusServiceUnavailable {
		t.Fatalf("sandbox 503: %d (%v), want 503", code, m)
	}
}

func writeMock(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
