package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

// recTestSetup — стенд + подключение + старт (stage/ai_text). TTS-кадры не
// нужны тесту (только текстовые события) — ttsPCMSize=0 ДО dial: приветствие
// и ответы ИИ без аудио-кадров (число кадров приветствия непредсказуемо:
// клиуза-диспетчизация + barge-in от нашего тона останавливает TTS на
// случайной границе).
func recTestSetup(t *testing.T) (*interviewEnv, *websocket.Conn, *mockVoice) {
	t.Helper()
	ts, token, sessionID, m, e := newVoiceEnv(t)
	m.ttsPCMSize = 0
	conn := dialWS(t, ts, token, sessionID)
	msgs := wsReadMixed(t, conn, 2, 3*time.Second)
	if msgs[0] != "text:stage/<nil>/<nil>" || !startsWith(msgs[1], "text:ai_text/") {
		t.Fatalf("старт: %v", msgs)
	}
	return e, conn, m
}

func recWritePCM(t *testing.T, conn *websocket.Conn, pcm []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = conn.Write(ctx, websocket.MessageBinary, pcm)
}

func recWriteRecording(t *testing.T, conn *websocket.Conn, on bool) {
	wsWriteJSON(t, conn, map[string]any{"type": "ui", "name": "recording", "payload": map[string]bool{"on": on}})
}

// recWaitSegment — прочитать следующий stt_segment (пропускает stt_partial;
// bin/TTS-кадры или ai_text — доказательство, что AI-ход стартовал → fail).
func recWaitSegment(t *testing.T, conn *websocket.Conn) string {
	t.Helper()
	for i := 0; i < 30; i++ {
		got := wsReadMixed(t, conn, 1, 5*time.Second)
		if len(got) == 0 {
			t.Fatal("stt_segment не пришёл")
		}
		switch {
		case strings.HasPrefix(got[0], "text:stt_segment/"):
			return got[0]
		case strings.HasPrefix(got[0], "text:stt_partial/"):
			continue // интерим — пропускаем
		default:
			t.Fatalf("в режиме записи пришёл не сегмент (AI-ход стартовал?): %v", got[0])
		}
	}
	t.Fatal("stt_segment: превышен лимит сообщений")
	return ""
}

// TestRecordingModeNoAutoTurn — режим записи (FR-S8, ADR-009): пока
// «включён микрофон» (recording on), стримовые STT-сегменты приходят как
// stt_segment и НЕ запускают AI-ход (нет transcript(user)/ai_text/TTS);
// явная отправка (recording off + utterance — склеенный текст) — один
// ход кандидата с ответом ИИ. Детерминировано (mock voice/LLM), стрим-путь.
// «Ход не стартовал» проверяется без таймаут-ридов (nhooyr закрывает conn
// по ctx-таймауту): явная отправка имеет УНИКАЛЬНЫЙ текст — если бы AI-ход
// стартовал от сегмента, busy-гейт отбросил бы utterance и его transcript
// не пришёл бы (а ai_text/bин-кадр пришёл бы раньше → fail в recWaitSegment).
func TestRecordingModeNoAutoTurn(t *testing.T) {
	_, conn, _ := recTestSetup(t)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	const explicit = "Склеенный транскрипт записи: расскажите о себе"

	recWriteRecording(t, conn, true)
	// Говорим: 4 кадра тона (1 с) + тишина (mock-хвост 250 мс → final).
	for i := 0; i < 4; i++ {
		recWritePCM(t, conn, tonePCM(250, 5000))
	}
	for i := 0; i < 2; i++ {
		recWritePCM(t, conn, silencePCM(250))
	}

	// Сегмент записи пришёл; AI-ход от него НЕ стартовал (recWaitSegment
	// упал бы на ai_text/bine, busy-гейт отбросил бы явную отправку ниже).
	seg := recWaitSegment(t, conn)
	if !strings.Contains(seg, "Здравствуйте, расскажите о себе") {
		t.Fatalf("текст сегмента: %q", seg)
	}

	// Явная отправка: recording off + utterance со склеенным текстом
	// (WS-порядок: off обрабатывается раньше utterance).
	recWriteRecording(t, conn, false)
	wsWriteJSON(t, conn, map[string]any{"type": "ui", "name": "utterance", "payload": map[string]string{"text": explicit}})

	// Ход: transcript(user) с НАШИМ текстом + ai_text (ответ ИИ).
	sawUser, sawAIText := false, false
	for i := 0; i < 30 && !(sawUser && sawAIText); i++ {
		got := wsReadMixed(t, conn, 1, 5*time.Second)
		if len(got) == 0 {
			t.Fatalf("явная отправка: transcript/ai_text не пришли (user=%v ai_text=%v)", sawUser, sawAIText)
		}
		switch {
		case got[0] == "text:transcript/user/"+explicit:
			sawUser = true
		case strings.HasPrefix(got[0], "text:ai_text/"):
			sawAIText = true
		case strings.HasPrefix(got[0], "text:transcript/user/"):
			t.Fatalf("до явной отправки пришёл чужой user-транскрипт (AI-ход от сегмента?): %v", got[0])
		}
	}
	if !sawUser || !sawAIText {
		t.Fatalf("явная отправка: transcript(user)/ai_text не пришли (user=%v ai_text=%v)", sawUser, sawAIText)
	}
}

// recWaitMsg — следующее текстовое WS-сообщение данного типа (сырой JSON:
// wsReadMixed форматирует только type/who/text, а нам нужны числовые поля).
// transcript(user)/ai_text в режиме записи = стартовавший AI-ход fail.
func recWaitMsg(t *testing.T, conn *websocket.Conn, typ string) map[string]any {
	t.Helper()
	for i := 0; i < 30; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		msgType, data, err := conn.Read(ctx)
		cancel()
		if err != nil {
			t.Fatalf("%s: ws read: %v", typ, err)
		}
		if msgType != websocket.MessageText {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		switch m["type"] {
		case typ:
			return m
		case "ai_text":
			t.Fatalf("в режиме записи стартовал AI-ход: %v", m)
		case "transcript":
			if m["who"] == "user" {
				t.Fatalf("в режиме записи пришёл user-транскрипт (AI-ход от сегмента?): %v", m)
			}
		}
	}
	t.Fatalf("%s: превышен лимит сообщений", typ)
	return nil
}

// TestRecordingPartialCarriesSpeechTotal (ui-review 2026-11-28): суммарная речь
// идёт уже в stt_partial — накопленное по завершённым сегментам + текущий
// сегмент. Раньше total_speech_ms жил только в stt_segment (граница тишины),
// и счётчик «речи N с» в окне записи стоял на нуле, пока кандидат говорит,
// а потом прыгал сразу на 7 с через 2-3,6 с после паузы.
func TestRecordingPartialCarriesSpeechTotal(t *testing.T) {
	_, conn, _ := recTestSetup(t)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	num := func(m map[string]any, key string) int64 {
		v, ok := m[key].(float64)
		if !ok {
			t.Fatalf("%s нет в %v", key, m)
		}
		return int64(v)
	}

	recWriteRecording(t, conn, true)
	// Сегмент 1: тон 4 кадра (1 с) + тишина partial (500 мс) и final (1000 мс).
	for i := 0; i < 4; i++ {
		recWritePCM(t, conn, tonePCM(250, 5000))
	}
	recWritePCM(t, conn, silencePCM(250))
	if got := num(recWaitMsg(t, conn, "stt_partial"), "total_speech_ms"); got != 500 {
		t.Fatalf("partial первого сегмента: total_speech_ms=%d, ждали 500", got)
	}
	if got := num(recWaitMsg(t, conn, "stt_segment"), "total_speech_ms"); got != 1000 {
		t.Fatalf("конец первого сегмента: total_speech_ms=%d, ждали 1000", got)
	}

	// Сегмент 2: тон 2 кадра (500 мс) partial обязан учесть прошлый сегмент.
	for i := 0; i < 2; i++ {
		recWritePCM(t, conn, tonePCM(250, 5000))
	}
	if got := num(recWaitMsg(t, conn, "stt_partial"), "total_speech_ms"); got != 1500 {
		t.Fatalf("partial второго сегмента: total_speech_ms=%d, ждали 1500", got)
	}
	recWritePCM(t, conn, silencePCM(250))
	recWriteRecording(t, conn, false)
}

// TestRecordingSegmentationOnSilence — длинная запись (FR-S8): несколько
// речевых сегментов (граница — тишина, VAD voice) → несколько stt_segment
// подряд, AI-ход не стартует ни на одном (склейка текста — на клиенте).
// Чанкование: сегментация по тишине уже в /stt/stream; жёсткий 30-с лимит
// непрерывной речи — в бэклоге (PROJECT_MEMORY).
func TestRecordingSegmentationOnSilence(t *testing.T) {
	_, conn, m := recTestSetup(t)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	recWriteRecording(t, conn, true)
	// Два речевых сегмента через тишину (границы чанков — на тишине).
	for _, burst := range []int{4, 3} {
		for i := 0; i < burst; i++ {
			recWritePCM(t, conn, tonePCM(250, 5000))
		}
		for i := 0; i < 2; i++ {
			recWritePCM(t, conn, silencePCM(250))
		}
	}

	// Два сегмента записи подряд; ни один не запустил AI-ход
	// (recWaitSegment fail'ится на ai_text/bin-кадре).
	recWaitSegment(t, conn)
	recWaitSegment(t, conn)
	if got := m.STTCalls(); got != 2 {
		t.Fatalf("stt-вызовов (final'ов): %d, ожидал 2", got)
	}
}

// TestRecordingUIEventsNoUnknown — регресс инцидента 2026-10-09:
// клиент (новый фронт) шлёт «recording»/«utterance», а запущенный
// бинарник api — старый (без case "recording" в handleUIEvent) →
// «unknown_ui_event: неизвестное событие: recording», а PCM идёт по
// старому авто-пайплайну (ответ на неполную реплику). Страхуем, что
// текущий сервер понимает события режима записи (error не приходит).
func TestRecordingUIEventsNoUnknown(t *testing.T) {
	_, conn, _ := recTestSetup(t)
	// recording on + utterance — сервер обязан обработать без ошибки.
	recWriteRecording(t, conn, true)
	wsWriteJSON(t, conn, map[string]any{"type": "ui", "name": "utterance", "payload": map[string]string{"text": "проверка событий режима записи"}})
	// Читаем до ai_text (AI-ход от явной отправки) и утверждаем: среди
	// событий нет error/unknown_ui_event.
	for i := 0; i < 30; i++ {
		got := wsReadMixed(t, conn, 1, 5*time.Second)
		if len(got) == 0 {
			t.Fatal("ai_text не пришёл")
		}
		if strings.Contains(got[0], "unknown_ui_event") || strings.HasPrefix(got[0], "text:error/") {
			t.Fatalf("ошибка на события режима записи: %v", got[0])
		}
		if strings.HasPrefix(got[0], "text:ai_text/") {
			return // события приняты, AI-ход стартовал — тест пройден
		}
	}
	t.Fatal("ai_text: превышен лимит сообщений")
}
