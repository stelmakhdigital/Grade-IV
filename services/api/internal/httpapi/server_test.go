package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/config"
)

func newTestEnv(t *testing.T) *httptest.Server {
	t.Helper()
	return newTestServer(t, nil)
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
	// Баланса минут больше нет: длительность интервью — метрика, не валюта.
	if v, ok := m["minutes_remaining_s"]; ok {
		t.Fatalf("register: минуты больше не выдаются, got %v", v)
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

	// /me с валидным токеном → 200 + email.
	code, m = doJSON(t, "GET", ts.URL+"/api/v1/auth/me", nil,
		map[string]string{"Authorization": "Bearer " + token})
	if code != http.StatusOK {
		t.Fatalf("me: status = %d, want 200 (body: %v)", code, m)
	}
	user, _ := m["user"].(map[string]any)
	if user["email"] != "candidate@example.ru" {
		t.Fatalf("me: email = %v, want candidate@example.ru", user["email"])
	}
	if _, ok := m["minutes_remaining_s"]; ok {
		t.Fatalf("me: баланс минут убран из ответа, got %v", m["minutes_remaining_s"])
	}
}

// TestDebugMicReportFlag: /debug/mic-report регистрируется ТОЛЬКО при
// ENABLE_DEBUG (cfg.DebugEndpoints): по умолчанию 404, с флагом — 204 + файл.
func TestDebugMicReportFlag(t *testing.T) {
	// По умолчанию (флаг не задан) — эндпоинт отсутствует.
	tsDefault := newTestEnv(t)
	resp, err := http.Post(tsDefault.URL+"/debug/mic-report", "application/json", bytes.NewBufferString(`{"x":1}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("без флага: хочу 404, получил %d", resp.StatusCode)
	}

	// С флагом — эндпоинт жив, файл пишется в FOR_RUN/debug/ (относительно cwd).
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	ts := newTestServer(t, func(cfg *config.Config) { cfg.DebugEndpoints = true })

	code, _ := doJSON(t, "POST", ts.URL+"/debug/mic-report", map[string]any{"kind": "test"}, map[string]string{"Origin": "http://localhost:5173"})
	if code != http.StatusNoContent {
		t.Fatalf("с флагом: хочу 204, получил %d", code)
	}
	entries, err := os.ReadDir(filepath.Join(tmp, "FOR_RUN", "debug"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("ожидаю 1 файл в FOR_RUN/debug, err=%v n=%d", err, len(entries))
	}
}
