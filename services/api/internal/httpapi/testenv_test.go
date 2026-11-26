package httpapi

// Общая сборка тестового HTTP-сервера: cfg + sqlite в памяти + миграции +
// mock-LLM + httptest.NewServer, всё с очисткой через t.Cleanup. Отличия
// тестов передаются через mutate (правки cfg применяются до db.Open), чтобы
// не плодить дословные копии этого тела по тест-файлам.

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/config"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/llm"
)

// discardLogger — единственная заглушка логгера в пакете тестов.
func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

// newTestServerFull — общее тело сборки окружения. sandboxHandler != nil —
// сначала поднимается mock-сервер sandbox, его адрес становится cfg.SandboxURL
// (после mutate). log — логгер сервера (в тестах — discardLogger). tweak —
// необязательная доводка Server ПОСЛЕ NewWithLLM, но ДО создания httptest-сервера
// (Handler() фиксирует middleware при вызове, поэтому поля вроде registerLimit
// нужно менять именно здесь). Возвращает сервер, mock-провайдер LLM (можно
// перенастроить SetResponder), основной httptest-сервер и sandbox-сервер
// (nil, если handler не задан).
func newTestServerFull(t *testing.T, log *slog.Logger, sandboxHandler http.HandlerFunc,
	mutate func(*config.Config), tweak func(*Server)) (*Server, *llm.MockProvider, *httptest.Server, *httptest.Server) {
	t.Helper()
	var sandbox *httptest.Server
	if sandboxHandler != nil {
		sandbox = httptest.NewServer(sandboxHandler)
		t.Cleanup(sandbox.Close)
	}
	cfg := &config.Config{
		Addr:           ":0",
		DatabaseURL:    "sqlite://:memory:",
		JWTSecret:      "test-secret",
		JWTExpiryHours: 1,
		ResumeDir:      t.TempDir(), // файлы резюме не должны попадать в рабочее дерево
	}
	if mutate != nil {
		mutate(cfg)
	}
	database, dialect, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.Migrate(context.Background(), database, dialect); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if sandbox != nil {
		cfg.SandboxURL = sandbox.URL
	}
	mock := llm.NewMockProvider()
	srv := NewWithLLM(cfg, database, dialect, log, mock)
	if tweak != nil {
		tweak(srv)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, mock, ts, sandbox
}

// newTestServer — тестовый сервер с дефолтным cfg; mutate (может быть nil)
// правит cfg до db.Open.
func newTestServer(t *testing.T, mutate func(*config.Config)) *httptest.Server {
	t.Helper()
	_, _, ts, _ := newTestServerFull(t, discardLogger(), nil, mutate, nil)
	return ts
}

// newTestServerWithSandbox — то же плюс mock-сервер sandbox (cfg.SandboxURL
// указывает на него); возвращает основной и sandbox-серверы.
func newTestServerWithSandbox(t *testing.T, sandboxHandler http.HandlerFunc,
	mutate func(*config.Config)) (ts *httptest.Server, sandbox *httptest.Server) {
	t.Helper()
	_, _, ts, sandbox = newTestServerFull(t, discardLogger(), sandboxHandler, mutate, nil)
	return ts, sandbox
}
