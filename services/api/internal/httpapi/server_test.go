package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/config"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
)

func newTestEnv(t *testing.T) *httptest.Server {
	t.Helper()
	cfg := &config.Config{
		Addr:           ":0",
		DatabaseURL:    "sqlite://:memory:",
		JWTSecret:      "test-secret",
		JWTExpiryHours: 1,
		MinutesFreeS:   3600,
	}
	database, dialect, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.Migrate(context.Background(), database, dialect); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	logger := slog.New(slog.DiscardHandler)
	srv := New(cfg, database, dialect, logger)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func doJSON(t *testing.T, method, url string, body any, headers map[string]string) (int, map[string]any) {
	t.Helper()
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var m map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("unmarshal %s: %v (body: %s)", url, err, raw)
		}
	}
	return resp.StatusCode, m
}

func TestHealth(t *testing.T) {
	ts := newTestEnv(t)
	code, m := doJSON(t, "GET", ts.URL+"/healthz", nil, nil)
	if code != http.StatusOK {
		t.Fatalf("health: status = %d, want 200", code)
	}
	if m["status"] != "ok" {
		t.Fatalf("health: status = %v, want ok", m["status"])
	}
}

func TestRegisterLoginMe(t *testing.T) {
	ts := newTestEnv(t)

	// Регистрация (email нормализуется к нижнему регистру).
	code, m := doJSON(t, "POST", ts.URL+"/api/v1/auth/register",
		map[string]string{"email": "Candidate@Example.ru", "password": "password1"}, nil)
	if code != http.StatusCreated {
		t.Fatalf("register: status = %d, want 201 (body: %v)", code, m)
	}
	token, _ := m["token"].(string)
	if token == "" {
		t.Fatal("register: пустой token")
	}
	if m["minutes_remaining_s"] != float64(3600) {
		t.Fatalf("register: minutes = %v, want 3600", m["minutes_remaining_s"])
	}

	// Дублирование email → 409.
	code, _ = doJSON(t, "POST", ts.URL+"/api/v1/auth/register",
		map[string]string{"email": "candidate@example.ru", "password": "password1"}, nil)
	if code != http.StatusConflict {
		t.Fatalf("register duplicate: status = %d, want 409", code)
	}

	// Слабый пароль → 400.
	code, _ = doJSON(t, "POST", ts.URL+"/api/v1/auth/register",
		map[string]string{"email": "weak@example.ru", "password": "123"}, nil)
	if code != http.StatusBadRequest {
		t.Fatalf("register weak: status = %d, want 400", code)
	}

	// Неверный пароль → 401.
	code, _ = doJSON(t, "POST", ts.URL+"/api/v1/auth/login",
		map[string]string{"email": "candidate@example.ru", "password": "wrong-pass"}, nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("login wrong: status = %d, want 401", code)
	}

	// Корректный логин → 200 + токен.
	code, m = doJSON(t, "POST", ts.URL+"/api/v1/auth/login",
		map[string]string{"email": "candidate@example.ru", "password": "password1"}, nil)
	if code != http.StatusOK {
		t.Fatalf("login: status = %d, want 200 (body: %v)", code, m)
	}

	// /me без токена → 401.
	code, _ = doJSON(t, "GET", ts.URL+"/api/v1/auth/me", nil, nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("me without token: status = %d, want 401", code)
	}

	// /me с битым токеном → 401.
	code, _ = doJSON(t, "GET", ts.URL+"/api/v1/auth/me", nil,
		map[string]string{"Authorization": "Bearer broken.token.here"})
	if code != http.StatusUnauthorized {
		t.Fatalf("me broken token: status = %d, want 401", code)
	}

	// /me с валидным токеном → 200 + email + баланс.
	code, m = doJSON(t, "GET", ts.URL+"/api/v1/auth/me", nil,
		map[string]string{"Authorization": "Bearer " + token})
	if code != http.StatusOK {
		t.Fatalf("me: status = %d, want 200 (body: %v)", code, m)
	}
	user, _ := m["user"].(map[string]any)
	if user["email"] != "candidate@example.ru" {
		t.Fatalf("me: email = %v, want candidate@example.ru", user["email"])
	}
	if m["minutes_remaining_s"] != float64(3600) {
		t.Fatalf("me: minutes = %v, want 3600", m["minutes_remaining_s"])
	}
}
