package httpapi

// Robustness паузы/возобновления (FR-S7): HTTP-тесты жизненного цикла
// (pause/resume/порог паузы) и поведение голосового контура в паузе
// (активный TTS-стрим останавливается, новые реплики — текстовые и голосовые —
// не обрабатываются).

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/config"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/llm"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/voicesvc"
	"nhooyr.io/websocket"
)

// postSessionAction — POST /api/v1/sessions/{id}/{action}; возвращает статус и тело.
func postSessionAction(t *testing.T, ts *httptest.Server, token string, id int64, action string) (int, map[string]any) {
	t.Helper()
	code, raw := doRaw(t, "POST", ts.URL+fmt.Sprintf("/api/v1/sessions/%d/%s", id, action), nil, nil, token)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return code, m
}

// TestSessionPauseResumeActions — HTTP-переходы: active→pause (200/paused),
// pause повторный (409/invalid_state), paused→resume (200/active),
// resume active-сессии (409/invalid_state).
func TestSessionPauseResumeActions(t *testing.T) {
	ts := newTestEnv(t)
	token := registerAndGetToken(t, ts)
	id, _ := createSession(t, ts, token, "junior", "go")

	// active → pause: 200, status=paused.
	code, m := postSessionAction(t, ts, token, id, "pause")
	if code != http.StatusOK {
		t.Fatalf("pause: %d (%v)", code, m)
	}
	if m["status"] != "paused" {
		t.Fatalf("pause: status = %v, want paused", m["status"])
	}

	// pause уже paused-сессии → 409 invalid_state.
	code, m = postSessionAction(t, ts, token, id, "pause")
	if code != http.StatusConflict {
		t.Fatalf("повторный pause: %d, want 409 (%v)", code, m)
	}
	if m["code"] != "invalid_state" {
		t.Fatalf("повторный pause: code = %v, want invalid_state", m["code"])
	}

	// paused → resume: 200, status=active.
	code, m = postSessionAction(t, ts, token, id, "resume")
	if code != http.StatusOK {
		t.Fatalf("resume: %d (%v)", code, m)
	}
	if m["status"] != "active" {
		t.Fatalf("resume: status = %v, want active", m["status"])
	}

	// resume active-сессии → 409 invalid_state.
	code, m = postSessionAction(t, ts, token, id, "resume")
	if code != http.StatusConflict {
		t.Fatalf("resume active: %d, want 409 (%v)", code, m)
	}
	if m["code"] != "invalid_state" {
		t.Fatalf("resume active: code = %v, want invalid_state", m["code"])
	}
}

// newTestEnvShortPauseTimeout — как newTestEnv, но SESSION_PAUSE_TIMEOUT_S = 1
// (порог паузы: обрыв без возобновления — вместо 30 мин, скорость теста;
// эквивалентно fake clock из engine_test.go на HTTP-уровне).
func newTestEnvShortPauseTimeout(t *testing.T) *httptest.Server {
	t.Helper()
	cfg := &config.Config{
		Addr:           ":0",
		DatabaseURL:    "sqlite://:memory:",
		JWTSecret:      "test-secret",
		JWTExpiryHours: 1,
		MinutesFreeS:   3600,
		PauseTimeoutS:  1,
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
	srv := NewWithLLM(cfg, database, dialect, logger, llm.NewMockProvider())
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// TestSessionPauseTimeoutResumeAborted — пауза дольше порога: resume прерывает
// сессию (aborted), ответ 409 invalid_state; GET подтверждает aborted.
func TestSessionPauseTimeoutResumeAborted(t *testing.T) {
	ts := newTestEnvShortPauseTimeout(t)
	token := registerAndGetToken(t, ts)
	id, _ := createSession(t, ts, token, "junior", "go")

	code, m := postSessionAction(t, ts, token, id, "pause")
	if code != http.StatusOK || m["status"] != "paused" {
		t.Fatalf("pause: %d (%v)", code, m)
	}

	// Пауза дольше порога (1 с).
	time.Sleep(1300 * time.Millisecond)

	// resume → 409 invalid_state (сессия прервана: aborted).
	code, m = postSessionAction(t, ts, token, id, "resume")
	if code != http.StatusConflict {
		t.Fatalf("resume после порога: %d, want 409 (%v)", code, m)
	}
	if m["code"] != "invalid_state" {
		t.Fatalf("resume после порога: code = %v, want invalid_state", m["code"])
	}

	// Статус — aborted (финализация с фактическим активным временем).
	s := getOwnedSession(t, ts, token, id)
	if s["status"] != "aborted" {
		t.Fatalf("статус после resume-после-порога: %v, want aborted", s)
	}
}

// consumeGreeting — старт сессии: stage + ai_text приветствия + TTS до end-кадра.
func consumeGreeting(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	msgs := wsReadMixed(t, conn, 2, 3*time.Second)
	if msgs[0] != "text:stage/<nil>/<nil>" || !startsWith(msgs[1], "text:ai_text/") {
		t.Fatalf("старт: %v", msgs)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		typ, _, fr, ok := bargeInReadOne(t, conn)
		if !ok {
			continue
		}
		if typ == "bin" && fr.end {
			return
		}
	}
	t.Fatal("TTS-приветствие не завершено end-кадром")
}

// pauseSession — HTTP-pause + чтение сообщений паузы: timer (движок шлёт его при
// паузе) и tts_stop (раунд 2: session-level стоп TTS — теперь всегда; порядок
// сообщений не гарантирован, ждём оба).
func pauseSession(t *testing.T, ts *httptest.Server, token string, id int64, conn *websocket.Conn) {
	t.Helper()
	code, m := postSessionAction(t, ts, token, id, "pause")
	if code != http.StatusOK {
		t.Fatalf("pause: %d (%v)", code, m)
	}
	if m["status"] != "paused" {
		t.Fatalf("pause: status = %v, want paused", m["status"])
	}
	var sawTimer, sawTTSStop bool
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !(sawTimer && sawTTSStop) {
		typ, _, _, ok := bargeInReadOne(t, conn)
		if !ok {
			continue
		}
		switch typ {
		case "timer":
			sawTimer = true
		case "tts_stop":
			sawTTSStop = true
		}
	}
	if !sawTimer || !sawTTSStop {
		t.Fatalf("после pause: timer=%v tts_stop=%v", sawTimer, sawTTSStop)
	}
}

// TestSessionPauseStopsActiveTTSStream — pause во время TTS-стрима хода ИИ:
// (1) WS tts_stop + end-кадр прерванного стрима; (2) новых PCM-кадров
// прерываемой реплики после end-кадра нет (воркеры синтеза не выпускают).
func TestSessionPauseStopsActiveTTSStream(t *testing.T) {
	ts, token, sessionID, conn, _ := bargeInEnv(t)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	// Пауза (HTTP), пока TTS-стрим хода ИИ идёт (первые 2 кадра прочитаны в прологе).
	pauseSession(t, ts, token, sessionID, conn)

	// (1) tts_stop подтверждён в pauseSession; ждём end-кадр прерванного стрима.
	sawEnd := false
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && !sawEnd {
		typ, _, fr, ok := bargeInReadOne(t, conn)
		if !ok {
			continue
		}
		if typ == "bin" && fr.end {
			sawEnd = true
		}
	}
	if !sawEnd {
		t.Fatal("прерванный TTS-поток не завершён end-кадром")
	}

	// (2) После end-кадра новых PCM-кадров нет: воркеры синтеза не выпускают
	// кадры (ai_text прерванного хода ещё может дойти — текстовые кадры OK).
	// Окончание теста: таймаут чтения обрывает соединение — допустимо.
	idleEnd := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(idleEnd) {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		typ, data, err := conn.Read(ctx)
		cancel()
		if err != nil {
			break // тишина — соединение оборвано таймаутом, тест завершён
		}
		if typ == websocket.MessageBinary {
			t.Fatalf("PCM-кадр после end-кадра прерванного стрима (len=%d) — поток не остановлен", len(data))
		}
	}
}

// TestSessionPauseDuringLLMPhase — R2: гонка окна. Ход начался (статус-гард
// прошёл), LLM ещё стримит, TTS ещё не начался (beginTTS не достигнут) — pause.
// Ожидание: клиент получает WS tts_stop и НИ ОДНОГО TTS-кадра хода (beginTTS при
// флаге sessStopped возвращает уже закрытый канал — стрим не стартует).
func TestSessionPauseDuringLLMPhase(t *testing.T) {
	e := newInterviewEnv(t)
	// LLM: медленный запрос до первого байта (2 с) — детерминированное окно
	// «LLM-фазы»: ход прошёл статус-гард, но beginTTS ещё не достигнут.
	e.mock.SetResponder(func(llm.Request) (string, error) {
		return "Первое предложение ответа. Второе предложение ответа. Третье предложение ответа.", nil
	})
	e.mock.SetStreamReqDelay(2 * time.Second)

	m := &mockVoice{ttsPCMSize: 96000} // 3 с аудио на предложение (если бы TTS стартовал)
	e.srv.cfg.VoiceURL = m.server(t).URL
	e.srv.voice = voicesvc.NewClient(e.srv.cfg.VoiceURL)

	conn := dialWS(t, e.ts, e.token, e.session)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	// Приветствие (не-стриминг Chat — быстрое): stage + ai_text + TTS до end-кадра.
	msgs := wsReadMixed(t, conn, 2, 3*time.Second)
	if msgs[0] != "text:stage/<nil>/<nil>" || !startsWith(msgs[1], "text:ai_text/") {
		t.Fatalf("старт: %v", msgs)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		typ, _, fr, ok := bargeInReadOne(t, conn)
		if !ok {
			continue
		}
		if typ == "bin" && fr.end {
			break
		}
	}

	// Реплика кандидата → ход (LLM-стрим ~2–4 с; TTS пока не начался).
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	_ = conn.Write(ctx, websocket.MessageText, mustJSON(
		map[string]any{"type": "ui", "name": "utterance", "payload": map[string]string{"text": "Привет"}}))
	cancel()

	// Ждём transcript(user) (ход прошёл статус-гард, LLM-фаза началась),
	// затем паузу ДО старта TTS (LLM ещё стримит — окно 300 мс << 2 с).
	var sawUserTranscript bool
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !sawUserTranscript {
		typ, who, _, ok := bargeInReadOne(t, conn)
		if !ok {
			continue
		}
		if typ == "transcript" && who == "user" {
			sawUserTranscript = true
		}
	}
	if !sawUserTranscript {
		t.Fatal("нет transcript(user) — ход не стартовал")
	}
	time.Sleep(400 * time.Millisecond) // LLM-фаза (первый токен мгновенно, ответ ~2 с)

	pauseSession(t, e.ts, e.token, e.session, conn) // tts_stop подтверждён в хелпере

	// TTS-кадров хода НЕ должно быть (окно 4 с: LLM-запрос 2 с + beginTTS с
	// уже закрытым каналом; приветствие уже дошло до end-кадра).
	deadline = time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		typ, _, _, ok := bargeInReadOne(t, conn)
		if !ok {
			continue
		}
		if typ == "bin" {
			t.Fatal("TTS-кадр после паузы в LLM-фазе — стрим начался, гонка не закрыта")
		}
	}
	// Остаток LLM-стрима: transcript(ai)/ai_text могут дойти (текст), кадров TTS — нет.
	idleEnd := time.Now().Add(2 * time.Second)
	for time.Now().Before(idleEnd) {
		c, c2 := context.WithTimeout(context.Background(), 500*time.Millisecond)
		typ, data, err := conn.Read(c)
		c2()
		if err != nil {
			break
		}
		if typ == websocket.MessageBinary {
			t.Fatalf("PCM-кадр TTS хода после паузы в LLM-фазе (len=%d)", len(data))
		}
	}
}

// TestSessionFinishDuringTTSStream — R3: finish во время активного TTS-стрима:
// WS tts_stop + end-кадр прерванного стрима, после end-кадра PCM-кадров нет.
func TestSessionFinishDuringTTSStream(t *testing.T) {
	ts, token, sessionID, conn, _ := bargeInEnv(t)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	// Finish (HTTP), пока TTS-стрим хода ИИ идёт (первые 2 кадра прочитаны в прологе).
	code, m := postSessionAction(t, ts, token, sessionID, "finish")
	if code != http.StatusOK {
		t.Fatalf("finish: %d (%v)", code, m)
	}
	if m["status"] != "finished" {
		t.Fatalf("finish: status = %v, want finished", m["status"])
	}

	// tts_stop + end-кадр прерванного стрима.
	sawTTSStop, sawEnd := false, false
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && !(sawTTSStop && sawEnd) {
		typ, _, fr, ok := bargeInReadOne(t, conn)
		if !ok {
			continue
		}
		switch typ {
		case "tts_stop":
			sawTTSStop = true
		case "bin":
			if fr.end {
				sawEnd = true
			}
		}
	}
	if !sawTTSStop {
		t.Fatal("нет WS tts_stop после finish")
	}
	if !sawEnd {
		t.Fatal("прерванный TTS-поток не завершён end-кадром")
	}

	// После end-кадра PCM-кадров нет (воркеры/paceFrames остановлены флагом).
	idleEnd := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(idleEnd) {
		c, c2 := context.WithTimeout(context.Background(), 500*time.Millisecond)
		typ, data, err := conn.Read(c)
		c2()
		if err != nil {
			break // тишина — соединение оборвано таймаутом, тест завершён
		}
		if typ == websocket.MessageBinary {
			t.Fatalf("PCM-кадр после end-кадра прерванного finish-стрима (len=%d)", len(data))
		}
	}
}

// TestSessionPauseIgnoresTextUtterance — paused-сессия не обрабатывает
// текстовые реплики кандидата (readLoop "utterance"): нет transcript/хода/TTS.
func TestSessionPauseIgnoresTextUtterance(t *testing.T) {
	ts, token, sessionID, m, _ := newVoiceEnv(t)
	m.ttsPCMSize = 48000
	conn := dialWS(t, ts, token, sessionID)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	consumeGreeting(t, conn)
	pauseSession(t, ts, token, sessionID, conn)

	// Текстовая реплика во время паузы.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	_ = conn.Write(ctx, websocket.MessageText, mustJSON(
		map[string]any{"type": "ui", "name": "utterance", "payload": map[string]string{"text": "вопрос во время паузы"}}))
	cancel()

	// Пауза: хода нет — новых сообщений не ожидается (окно 1.5 с; таймаут
	// чтения обрывает соединение — тест завершён).
	idleCtx, idleCancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	_, data, rerr := conn.Read(idleCtx)
	idleCancel()
	if rerr == nil {
		t.Fatalf("сообщение во время паузы после реплики кандидата — ход обработан: %s", string(data))
	}
}

// TestSessionPauseIgnoresVoiceUtterance — paused-сессия не обрабатывает
// голосовые реплики (PCM не доходит до VAD/STT): нет transcript/хода, STT
// не вызывался.
func TestSessionPauseIgnoresVoiceUtterance(t *testing.T) {
	ts, token, sessionID, m, _ := newVoiceEnv(t)
	m.ttsPCMSize = 48000
	conn := dialWS(t, ts, token, sessionID)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	consumeGreeting(t, conn)
	pauseSession(t, ts, token, sessionID, conn)

	sttBefore := m.sttCalls
	// «Реплика»: тон 2×250 мс + тишина 3×250 мс (формат реплики barge-in-тестов).
	writePCM := func(pcm []byte) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = conn.Write(ctx, websocket.MessageBinary, pcm)
		cancel()
	}
	for i := 0; i < 2; i++ {
		writePCM(tonePCM(250, 5000))
	}
	for i := 0; i < 3; i++ {
		writePCM(silencePCM(250))
	}

	// Пауза: хода нет — новых сообщений не ожидается (окно 1.5 с).
	idleCtx, idleCancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	_, data, rerr := conn.Read(idleCtx)
	idleCancel()
	if rerr == nil {
		t.Fatalf("сообщение во время паузы после голосовой реплики — ход обработан: %s", string(data))
	}
	if m.sttCalls != sttBefore {
		t.Fatalf("STT вызывался во время паузы (calls: %d → %d)", sttBefore, m.sttCalls)
	}
}
