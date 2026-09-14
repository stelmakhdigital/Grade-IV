package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/config"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/llm"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
	"nhooyr.io/websocket"
)

// discardLog — логгер тестов (по умолчанию тишина; VOCE_TEST_LOG=1 — в stderr).
var discardLog = slog.New(slog.NewTextHandler(io.Discard, nil))

// interviewEnv — окружение с управляемым mock-LLM (NewWithLLM).
type interviewEnv struct {
	ts      *httptest.Server
	srv     *Server
	mock    *llm.MockProvider
	token   string
	session int64
}

func newInterviewEnv(t *testing.T) *interviewEnv {
	t.Helper()
	if os.Getenv("VOCE_TEST_LOG") == "1" {
		discardLog = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	cfg := &config.Config{
		Addr:           ":0",
		DatabaseURL:    "sqlite://:memory:",
		JWTSecret:      "test-secret",
		JWTExpiryHours: 1,
		MinutesFreeS:   3600,
		SilenceNudgeS:  1,
		SandboxURL:     "http://127.0.0.1:1", // нет sandbox: задачи не выдаются
	}
	database, dialect, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.Migrate(context.Background(), database, dialect); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	mock := llm.NewMockProvider()
	srv := NewWithLLM(cfg, database, dialect, discardLog, mock)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	e := &interviewEnv{ts: ts, srv: srv, mock: mock}
	code, m := doJSON(t, "POST", ts.URL+"/api/v1/auth/register",
		map[string]string{"email": "iv@example.com", "password": "password1"}, nil)
	if code != http.StatusCreated {
		t.Fatalf("register: %d", code)
	}
	e.token, _ = m["token"].(string)
	sid, _ := createSession(t, ts, e.token, "middle", "go")
	e.session = sid
	return e
}

func TestInterviewerUtterance(t *testing.T) {
	e := newInterviewEnv(t)
	ctx := context.Background()

	text, err := e.srv.interviewer.OnUserUtterance(ctx, e.session, "Здравствуйте, я готов начинать.")
	if err != nil {
		t.Fatalf("utterance: %v", err)
	}
	if text == "" {
		t.Fatal("пустой ответ")
	}
	// system-промпт: персона + грейд middle + стадия voice.
	sys := e.mock.LastSystem()
	for _, part := range []string{"интервьюер", "Middle", "голосовое интервью", "Стек: go"} {
		if !strings.Contains(sys, part) {
			t.Errorf("system-промпт без %q: %s", part, sys)
		}
	}
	// В контексте — реплика кандидата (последнее user-сообщение).
	calls := e.mock.Calls()
	if len(calls) != 1 {
		t.Fatalf("кол-во вызовов LLM: %d", len(calls))
	}
	last := calls[0].Messages[len(calls[0].Messages)-1]
	if last.Role != llm.RoleUser || last.Content != "Здравствуйте, я готов начинать." {
		t.Fatalf("последнее сообщение: %+v", last)
	}
	// Событие ai_utterance записано.
	events, _ := e.srv.sessions.ListEvents(ctx, e.session)
	var found bool
	for _, ev := range events {
		if ev.Kind == "ai_utterance" {
			found = true
		}
	}
	if !found {
		t.Fatal("нет события ai_utterance")
	}
}

func TestInterviewerTranscript(t *testing.T) {
	e := newInterviewEnv(t)
	ctx := context.Background()
	_, _ = e.srv.interviewer.OnUserUtterance(ctx, e.session, "Первая реплика.")
	// Во втором вызове контекст содержит предыдущие реплики (user + assistant).
	_, _ = e.srv.interviewer.OnUserUtterance(ctx, e.session, "Вторая реплика.")
	calls := e.mock.Calls()
	msgs := calls[1].Messages
	var userCount, asstCount int
	for _, m := range msgs[1:] { // минус system
		switch m.Role {
		case llm.RoleUser:
			userCount++
		case llm.RoleAssistant:
			asstCount++
		}
	}
	// Второй ход: в контексте предыдущий ответ ИИ (assistant) + текущая реплика (user).
	// (user_utterance-событие первого хода пишет WS-хендлер, здесь — прямой вызов.)
	if userCount < 1 || asstCount < 1 {
		t.Fatalf("транскрипт не передан: user=%d assistant=%d (%+v)", userCount, asstCount, msgs)
	}
}

func TestInterviewerCodeRunStageGate(t *testing.T) {
	e := newInterviewEnv(t)
	ctx := context.Background()
	// На voice-стадии ревью кода запрещено.
	if _, err := e.srv.interviewer.OnCodeRun(ctx, e.session, map[string]any{"passed": true}); err == nil {
		t.Fatal("ожидалась ErrWrongStage")
	}
	// На livecode — проходит.
	if _, err := e.srv.Engine().Transition(e.session, models.StageLiveCode); err != nil {
		t.Fatalf("transition: %v", err)
	}
	text, err := e.srv.interviewer.OnCodeRun(ctx, e.session, map[string]any{
		"passed": true, "exit_code": 0, "duration_ms": 100, "hint": "тесты прошли",
	})
	if err != nil || text == "" {
		t.Fatalf("codrun на livecode: %v (%q)", err, text)
	}
}

func TestInterviewerNudge(t *testing.T) {
	e := newInterviewEnv(t)
	ctx := context.Background()
	text, err := e.srv.interviewer.Nudge(ctx, e.session, 12)
	if err != nil || text == "" {
		t.Fatalf("nudge: %v (%q)", err, text)
	}
	events, _ := e.srv.sessions.ListEvents(ctx, e.session)
	var found bool
	for _, ev := range events {
		if ev.Kind == "ai_nudge" {
			found = true
		}
	}
	if !found {
		t.Fatal("нет события ai_nudge")
	}
}

func TestInterviewerTerminalSession(t *testing.T) {
	e := newInterviewEnv(t)
	ctx := context.Background()
	if _, err := e.srv.Engine().Finish(e.session); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if _, err := e.srv.interviewer.OnUserUtterance(ctx, e.session, "после финализации"); err == nil {
		t.Fatal("ожидалась ErrSessionEnded")
	}
}

// TestWSUtteranceFlow — полный WS-конвейер: utterance → transcript(user) →
// transcript(ai) → ai_text (mock-LLM) + событие user_utterance.
func TestWSUtteranceFlow(t *testing.T) {
	e := newInterviewEnv(t)
	conn := dialWS(t, e.ts, e.token, e.session)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	// Стартовое: stage voice (+ ai_text приветствия — сессия новая).
	types := wsReadTypes(t, conn, 2, 3*time.Second)
	if types[0] != "stage/voice" || !startsWith(types[1], "ai_text/") {
		t.Fatalf("старт: %v", types)
	}

	// Реплика кандидата → transcript(user), transcript(ai), ai_text.
	wsWriteJSON(t, conn, map[string]any{"type": "ui", "name": "utterance",
		"payload": map[string]string{"text": "Расскажи, с чего начать?"}})
	msgs := wsReadRaw(t, conn, 3, 3*time.Second)
	var whos, types2 []string
	for _, raw := range msgs {
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		types2 = append(types2, fmt.Sprint(m["type"]))
		whos = append(whos, fmt.Sprint(m["who"]))
	}
	if types2[0] != "transcript" || whos[0] != "user" {
		t.Fatalf("transcript user: %v %v", types2, whos)
	}
	if types2[1] != "transcript" || whos[1] != "ai" {
		t.Fatalf("transcript ai: %v %v", types2, whos)
	}
	if types2[2] != "ai_text" {
		t.Fatalf("ai_text: %v", types2)
	}

	// Событие user_utterance зафиксировано (эндпоинт /events — массив).
	req, _ := http.NewRequest("GET", e.ts.URL+fmt.Sprintf("/api/v1/sessions/%d/events", e.session), nil)
	req.Header.Set("Authorization", "Bearer "+e.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("events: %d", resp.StatusCode)
	}
	var events []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&events)
	found := false
	for _, ev := range events {
		if ev["kind"] == "user_utterance" {
			found = true
		}
	}
	if !found {
		t.Fatalf("нет события user_utterance: %v", events)
	}
}

// TestWSNudge — молчание > порога (1 с в тесте) → ai_text nudge.
func TestWSNudge(t *testing.T) {
	e := newInterviewEnv(t)
	conn := dialWS(t, e.ts, e.token, e.session)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	// Дренируем старт (stage + приветствие).
	_ = wsReadTypes(t, conn, 2, 3*time.Second)
	before := len(e.mock.Calls())

	// Молчим 2.5 с (порог 1 с) → хотя бы один nudge.
	time.Sleep(2500 * time.Millisecond)
	types := wsReadTypes(t, conn, 1, 3*time.Second)
	if !startsWith(types[0], "ai_text/") {
		t.Fatalf("nudge: %v", types)
	}
	if len(e.mock.Calls()) <= before {
		t.Fatal("nudge не обратился к LLM")
	}
}

// TestWSLiveCodeTask — вход на livecode: stage с task из банка (mock sandbox) + ai_text.
func TestWSLiveCodeTask(t *testing.T) {
	e := newInterviewEnv(t)
	// Подменяем SandboxURL на mock-банк задач.
	sbx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]string{{
			"id": "go-two-sum", "title": "Two Sum", "statement": "Найдите индексы двух чисел.",
		}})
	}))
	defer sbx.Close()
	e.srv.cfg.SandboxURL = sbx.URL

	conn := dialWS(t, e.ts, e.token, e.session)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
	_ = wsReadTypes(t, conn, 2, 3*time.Second) // старт

	wsWriteJSON(t, conn, map[string]any{"type": "ui", "name": "stage_action",
		"payload": map[string]string{"stage": "livecode"}})

	// stage (движок, без task) + stage (с task из банка) + ai_text (комментарий входа).
	types := wsReadTypes(t, conn, 3, 3*time.Second)
	if types[0] != "stage/livecode" || types[1] != "stage/livecode" || !startsWith(types[2], "ai_text/") {
		t.Fatalf("вход livecode: %v", types)
	}
	// Задача попала в контекст LLM (stageNote в последнем запросе).
	calls := e.mock.Calls()
	if len(calls) == 0 {
		t.Fatal("нет запросов к LLM")
	}
	last := calls[len(calls)-1]
	found := false
	for _, m := range last.Messages {
		if strings.Contains(m.Content, "Two Sum") {
			found = true
		}
	}
	if !found {
		t.Fatalf("задача не передана LLM: %+v", last.Messages)
	}
}

// wsReadRaw — прочитать n текстовых сообщений (сырые кадры), таймаут на всё.
func wsReadRaw(t *testing.T, conn *websocket.Conn, n int, timeout time.Duration) [][]byte {
	t.Helper()
	deadline := time.Now().Add(timeout)
	out := make([][]byte, 0, n)
	for len(out) < n {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		ctx, cancel := context.WithTimeout(context.Background(), remaining)
		typ, data, err := conn.Read(ctx)
		cancel()
		if err != nil {
			t.Fatalf("ws read raw (%d/%d): %v", len(out), n, err)
		}
		if typ != websocket.MessageText {
			t.Fatalf("ожидался текстовый кадр, получен %v", typ)
		}
		out = append(out, data)
	}
	return out
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
