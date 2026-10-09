package httpapi

import (
	"context"
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
