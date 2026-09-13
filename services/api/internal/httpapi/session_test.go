package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

// registerAndGetToken — зарегистрировать пользователя и вернуть JWT.
func registerAndGetToken(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	email := fmt.Sprintf("cand-%d@example.com", time.Now().UnixNano())
	code, m := doJSON(t, "POST", ts.URL+"/api/v1/auth/register",
		map[string]string{"email": email, "password": "password1"}, nil)
	if code != http.StatusCreated {
		t.Fatalf("register: status = %d (body: %v)", code, m)
	}
	token, _ := m["token"].(string)
	if token == "" {
		t.Fatal("register: пустой token")
	}
	return token
}

// doRaw — как doJSON, но возвращает сырые байты (для ответов-массивов).
func doRaw(t *testing.T, method, url string, body []byte, headers map[string]string, token string) (int, []byte) {
	t.Helper()
	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if req.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

// createSession — POST /api/v1/sessions; возвращает {id, duration_limit_s}.
func createSession(t *testing.T, ts *httptest.Server, token, grade, stack string) (int64, int) {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"grade": grade, "stack": stack})
	code, raw := doRaw(t, "POST", ts.URL+"/api/v1/sessions", b, nil, token)
	if code != http.StatusCreated {
		t.Fatalf("create session: status = %d (body: %s)", code, raw)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("create session: %v (%s)", err, raw)
	}
	id, _ := m["id"].(float64)
	limit, _ := m["duration_limit_s"].(float64)
	if id == 0 {
		t.Fatalf("create session: пустой id (%s)", raw)
	}
	if m["ws_url"] != "/ws/session/"+fmt.Sprint(int64(id)) {
		t.Fatalf("create session: ws_url = %v", m["ws_url"])
	}
	return int64(id), int(limit)
}

func getOwnedSession(t *testing.T, ts *httptest.Server, token string, id int64) map[string]any {
	t.Helper()
	code, raw := doRaw(t, "GET", ts.URL+fmt.Sprintf("/api/v1/sessions/%d", id), nil, nil, token)
	if code != http.StatusOK {
		t.Fatalf("get session: status = %d (body: %s)", code, raw)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("get session: %v", err)
	}
	return m
}

func TestSessionsREST(t *testing.T) {
	ts := newTestEnv(t)
	token := registerAndGetToken(t, ts)

	// Создание.
	id, limit := createSession(t, ts, token, "junior", "go")
	if limit != 2700 {
		t.Fatalf("duration limit: %d", limit)
	}

	// Состояние: voice/active, время осталось ~2700 с.
	s := getOwnedSession(t, ts, token, id)
	if s["stage"] != "voice" || s["status"] != "active" {
		t.Fatalf("start state: %v", s)
	}
	if tl, _ := s["time_left_s"].(float64); tl < 2695 || tl > 2700 {
		t.Fatalf("time_left: %v", s["time_left_s"])
	}

	// Список: одна сессия.
	code, raw := doRaw(t, "GET", ts.URL+"/api/v1/sessions", nil, nil, token)
	if code != http.StatusOK {
		t.Fatalf("list: %d", code)
	}
	var list []map[string]any
	if err := json.Unmarshal(raw, &list); err != nil || len(list) != 1 {
		t.Fatalf("list: %v (%s)", list, raw)
	}

	// pause → paused; resume → active.
	code, raw = doRaw(t, "POST", ts.URL+fmt.Sprintf("/api/v1/sessions/%d/pause", id), nil, nil, token)
	if code != http.StatusOK {
		t.Fatalf("pause: %d (%s)", code, raw)
	}
	var pm map[string]any
	_ = json.Unmarshal(raw, &pm)
	if pm["status"] != "paused" {
		t.Fatalf("pause: %v", pm)
	}
	code, raw = doRaw(t, "POST", ts.URL+fmt.Sprintf("/api/v1/sessions/%d/resume", id), nil, nil, token)
	if code != http.StatusOK {
		t.Fatalf("resume: %d (%s)", code, raw)
	}
	var rm map[string]any
	_ = json.Unmarshal(raw, &rm)
	if rm["status"] != "active" {
		t.Fatalf("resume: %v", rm)
	}

	// finish → finished/report, time_left 0.
	code, raw = doRaw(t, "POST", ts.URL+fmt.Sprintf("/api/v1/sessions/%d/finish", id), nil, nil, token)
	if code != http.StatusOK {
		t.Fatalf("finish: %d (%s)", code, raw)
	}
	var fm map[string]any
	_ = json.Unmarshal(raw, &fm)
	if fm["status"] != "finished" || fm["stage"] != "report" {
		t.Fatalf("finish: %v", fm)
	}
	if tl, _ := fm["time_left_s"].(float64); tl != 0 {
		t.Fatalf("time_left после finish: %v", fm["time_left_s"])
	}

	// Повторный finish → 409.
	code, _ = doRaw(t, "POST", ts.URL+fmt.Sprintf("/api/v1/sessions/%d/finish", id), nil, nil, token)
	if code != http.StatusConflict {
		t.Fatalf("повторный finish: %d, want 409", code)
	}

	// pause завершённой → 409.
	code, _ = doRaw(t, "POST", ts.URL+fmt.Sprintf("/api/v1/sessions/%d/pause", id), nil, nil, token)
	if code != http.StatusConflict {
		t.Fatalf("pause завершённой: %d, want 409", code)
	}

	// События: session_created в начале, finished — в конце.
	code, raw = doRaw(t, "GET", ts.URL+fmt.Sprintf("/api/v1/sessions/%d/events", id), nil, nil, token)
	if code != http.StatusOK {
		t.Fatalf("events: %d", code)
	}
	var events []struct {
		Seq  int    `json:"seq"`
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &events); err != nil || len(events) < 2 {
		t.Fatalf("events: %v (%s)", events, raw)
	}
	if events[0].Kind != "session_created" || events[len(events)-1].Kind != "finished" {
		t.Fatalf("events kinds: %+v", events)
	}

	// Чужая сессия → 404.
	token2 := registerAndGetToken(t, ts)
	code, _ = doRaw(t, "GET", ts.URL+fmt.Sprintf("/api/v1/sessions/%d", id), nil, nil, token2)
	if code != http.StatusNotFound {
		t.Fatalf("чужая сессия: %d, want 404", code)
	}

	// Несуществующая сессия → 404.
	code, _ = doRaw(t, "GET", ts.URL+"/api/v1/sessions/99999", nil, nil, token)
	if code != http.StatusNotFound {
		t.Fatalf("несуществующая: %d, want 404", code)
	}
}

// wsReadTypes читает до n сообщений и возвращает их "type/name".
func wsReadTypes(t *testing.T, conn *websocket.Conn, n int, timeout time.Duration) []string {
	t.Helper()
	out := make([]string, 0, n)
	deadline := time.Now().Add(timeout)
	for len(out) < n {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		ctx, cancel := context.WithTimeout(context.Background(), remaining)
		typ, data, err := conn.Read(ctx)
		cancel()
		if err != nil {
			t.Fatalf("ws read (%d/%d): %v (получено: %v)", len(out), n, err, out)
		}
		if typ != websocket.MessageText {
			t.Fatalf("ws: неожиданный тип кадра: %v", typ)
		}
		var msg map[string]any
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("ws: не JSON: %s", data)
		}
		out = append(out, fmt.Sprintf("%s/%v", msg["type"], msg["name"]))
	}
	return out
}

func wsWriteJSON(t *testing.T, conn *websocket.Conn, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("ws marshal: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatalf("ws write: %v", err)
	}
}

func dialWS(t *testing.T, ts *httptest.Server, token string, id int64) *websocket.Conn {
	t.Helper()
	u := url.URL{Scheme: "ws", Host: ts.Listener.Addr().String(),
		Path: fmt.Sprintf("/ws/session/%d", id), RawQuery: "token=" + url.QueryEscape(token)}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, u.String(), nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	return conn
}

func TestSessionWS(t *testing.T) {
	ts := newTestEnv(t)
	token := registerAndGetToken(t, ts)
	id, _ := createSession(t, ts, token, "middle", "python")

	conn := dialWS(t, ts, token, id)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	// Стартовое сообщение: stage voice.
	types := wsReadTypes(t, conn, 1, 3*time.Second)
	if types[0] != "stage/voice" {
		t.Fatalf("стартовое сообщение: %v", types)
	}

	// Бинарный PCM-кадр принимается без ошибки.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	if err := conn.Write(ctx, websocket.MessageBinary, make([]byte, 8000)); err != nil {
		t.Fatalf("ws write pcm: %v", err)
	}
	cancel()

	// ui: stage_action livecode → stage livecode.
	wsWriteJSON(t, conn, map[string]any{"type": "ui", "name": "stage_action",
		"payload": map[string]string{"stage": "livecode"}})
	types = wsReadTypes(t, conn, 1, 3*time.Second)
	if types[0] != "stage/livecode" {
		t.Fatalf("stage_action: %v", types)
	}

	// ui: недопустимый переход livecode→report (у Middle report — из design) → error.
	wsWriteJSON(t, conn, map[string]any{"type": "ui", "name": "stage_action",
		"payload": map[string]string{"stage": "report"}})
	types = wsReadTypes(t, conn, 1, 3*time.Second)
	if !strings.HasPrefix(types[0], "error/") {
		t.Fatalf("ожидалось error, получено: %v", types)
	}

	// ui: finish → timer(0) + stage(report).
	wsWriteJSON(t, conn, map[string]any{"type": "ui", "name": "finish", "payload": map[string]any{}})
	types = wsReadTypes(t, conn, 2, 3*time.Second)
	if types[0] != "timer/<nil>" || types[1] != "stage/report" {
		t.Fatalf("finish: %v", types)
	}

	// Сессия завершена: GET подтверждает finished.
	s := getOwnedSession(t, ts, token, id)
	if s["status"] != "finished" {
		t.Fatalf("после WS finish: %v", s)
	}
}

func TestSessionWSAuthAndDisconnect(t *testing.T) {
	ts := newTestEnv(t)
	token := registerAndGetToken(t, ts)
	id, _ := createSession(t, ts, token, "middle", "go")

	// Без токена → 401 (до апгрейда).
	req, _ := http.NewRequest("GET", ts.URL+fmt.Sprintf("/ws/session/%d", id), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ws no token: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("ws без токена: %d, want 401", resp.StatusCode)
	}

	// Подключение и немедленное отключение → сессия в паузе (FR-S7).
	conn := dialWS(t, ts, token, id)
	_ = wsReadTypes(t, conn, 1, 3*time.Second) // стартовое stage
	_ = conn.Close(websocket.StatusNormalClosure, "")

	deadline := time.Now().Add(2 * time.Second)
	for {
		s := getOwnedSession(t, ts, token, id)
		if s["status"] == "paused" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("обрыв не перевёл сессию в paused: %v", s)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
