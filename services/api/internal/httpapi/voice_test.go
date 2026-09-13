package httpapi

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/voicesvc"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

// mockVoice — voice-сервис для тестов: /stt (любой аудио → фиксированный текст),
// /tts (текст → известный PCM), /health. Считает вызовы.
type mockVoice struct {
	sttCalls   int
	ttsCalls   int
	ttsTexts   []string
	sttBytes   int
	ttsPCMSize int
}

func (m *mockVoice) server(t *testing.T) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/stt":
			m.sttCalls++
			body, _ := readAllLimited(r, 1<<20)
			m.sttBytes += len(body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"text":"Здравствуйте, расскажите о себе","confidence":0.87,"duration_s":1.6}`))
		case "/api/v1/tts":
			var p struct {
				Text string `json:"text"`
			}
			_ = json.NewDecoder(r.Body).Decode(&p)
			m.ttsCalls++
			m.ttsTexts = append(m.ttsTexts, p.Text)
			w.Header().Set("Content-Type", "audio/pcm")
			pcm := make([]byte, m.ttsPCMSize)
			for i := range pcm {
				v := int16(3000 * math.Sin(2*math.Pi*300*float64(i)/16000))
				pcm[i] = byte(v)
			}
			_, _ = w.Write(pcm)
		case "/api/v1/health":
			_, _ = w.Write([]byte(`{"service":"grade-voice","stt":{"provider":"fake"},"tts":{"provider":"fake"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	return ts
}

func readAllLimited(r *http.Request, n int64) ([]byte, error) {
	buf := make([]byte, 0, 64*1024)
	tmp := make([]byte, 4096)
	for {
		k, err := r.Body.Read(tmp)
		buf = append(buf, tmp[:k]...)
		if err != nil {
			break
		}
	}
	return buf, nil
}

// tonePCM — синтетическая «речь»: синус 440 Гц (RMS ~ amp/√2).
func tonePCM(ms, amp int) []byte {
	n := 16000 * ms / 1000
	pcm := make([]byte, n*2)
	for i := 0; i < n; i++ {
		v := int16(float64(amp) * math.Sin(2*math.Pi*440*float64(i)/16000))
		pcm[2*i] = byte(v)
		pcm[2*i+1] = byte(v >> 8)
	}
	return pcm
}

// silencePCM — тихий шум (RMS ~ amp/√2).
func silencePCM(ms int) []byte { return tonePCM(ms, 10) }

// newVoiceEnv — тестовый стенд: api (mock-LLM) + mock-voice.
func newVoiceEnv(t *testing.T) (ts *httptest.Server, token string, session int64, mock *mockVoice, env *interviewEnv) {
	t.Helper()
	e := newInterviewEnv(t)
	m := &mockVoice{ttsPCMSize: 48000} // 1.5 с
	e.srv.cfg.VoiceURL = m.server(t).URL
	e.srv.voice = voicesvc.NewClient(e.srv.cfg.VoiceURL) // клиент создан до подмены — пересоздаём
	e.srv.cfg.SilenceNudgeS = 300                        // nudge уже покрыт TestWSNudge — в конвейерных тестах не мешаем
	// Конфигурация VAD — как в проде, но хвост тишины короче для скорости теста.
	e.srv.cfg.VADEndSilenceMS = 500
	e.srv.cfg.VADRMSThreshold = 500
	return e.ts, e.token, e.session, m, e
}

// wsReadMixed — прочитать n сообщений: "text:<json-кратко>" или "bin:<len>".
func wsReadMixed(t *testing.T, conn *websocket.Conn, n int, timeout time.Duration) []string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	out := make([]string, 0, n)
	for len(out) < n {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		ctx, cancel := context.WithTimeout(context.Background(), remaining)
		typ, data, err := conn.Read(ctx)
		cancel()
		if err != nil {
			// Таймаут без сообщений — не ошибка (ожидание серии), а «нет кадров».
			if len(out) == 0 && errors.Is(err, context.DeadlineExceeded) {
				return out
			}
			t.Fatalf("ws read mixed (%d/%d): %v (получено: %v)", len(out), n, err, out)
		}
		if typ == websocket.MessageBinary {
			out = append(out, fmt.Sprintf("bin:%d", len(data)))
		} else {
			var m map[string]any
			_ = json.Unmarshal(data, &m)
			out = append(out, fmt.Sprintf("text:%s/%v/%v", m["type"], m["who"], m["text"]))
		}
	}
	return out
}

// TestWSVoicePipeline — полный контур ADR-002: PCM → VAD → STT(mock) → LLM(mock)
// → transcript/ai_text + TTS(mock) → бинарные кадры {seq,flags}+PCM.
// Примечание (nhooyr): Read с истёкшим ctx закрывает соединение — поэтому
// «висящие» чтения без гарантированных сообщений не используются: считаем
// кадры TTS по известному размеру PCM мок-а.
func TestWSVoicePipeline(t *testing.T) {
	ts, token, sessionID, m, e := newVoiceEnv(t)
	conn := dialWS(t, ts, token, sessionID)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	// 1) Старт: stage + приветствие (ai_text).
	msgs := wsReadMixed(t, conn, 2, 3*time.Second)
	if msgs[0] != "text:stage/<nil>/<nil>" {
		t.Fatalf("старт: %v", msgs)
	}
	if !startsWith(msgs[1], "text:ai_text/") {
		t.Fatalf("приветствие: %v", msgs[1])
	}

	// 2) TTS приветствия: кадры 8000 байт (+4 заголовок), количество = ceil(PCM/8000).
	frames := (m.ttsPCMSize + 7999) / 8000
	for i := 0; i < frames; i++ {
		got := wsReadMixed(t, conn, 1, 3*time.Second)
		if len(got) != 1 || got[0] != "bin:8004" {
			t.Fatalf("TTS-кадр %d: %v", i, got)
		}
	}

	// 3) Говорим: 4 кадра тона (1 с) + тишина (хвост 500 мс → VAD завершает).
	writePCM := func(pcm []byte) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = conn.Write(ctx, websocket.MessageBinary, pcm)
		cancel()
	}
	for i := 0; i < 4; i++ {
		writePCM(tonePCM(250, 5000))
	}
	for i := 0; i < 3; i++ {
		writePCM(silencePCM(250))
	}

	// 4) Ожидаем transcript(user) + transcript(ai) + ai_text (mock-ответ).
	wantReply := "[mock-интервьюер] принял реплику: Здравствуйте, расскажите о себе"
	var sawUser, sawAI, sawAIText bool
	for i := 0; i < 12 && !(sawUser && sawAI && sawAIText); i++ {
		got := wsReadMixed(t, conn, 1, 2*time.Second)
		if len(got) == 0 {
			continue
		}
		switch got[0] {
		case "text:transcript/user/Здравствуйте, расскажите о себе":
			sawUser = true
		case "text:transcript/ai/" + wantReply:
			sawAI = true
		case "text:ai_text/<nil>/" + wantReply:
			sawAIText = true
		}
	}
	if !sawUser {
		t.Fatal("нет transcript(user) от STT")
	}
	if !sawAI {
		t.Fatal("нет transcript(ai)")
	}
	if !sawAIText {
		t.Fatal("нет ai_text")
	}

	// 5) TTS ответа: снова frames бинарных кадров (читаем первые 2 — контур работает).
	for i := 0; i < 2; i++ {
		got := wsReadMixed(t, conn, 1, 3*time.Second)
		if len(got) != 1 || got[0] != "bin:8004" {
			t.Fatalf("TTS-кадр ответа %d: %v", i, got)
		}
	}

	// 6) STT вызывался; события user_utterance + ai_utterance в истории.
	if m.sttCalls < 1 {
		t.Fatalf("STT не вызывался (calls=%d)", m.sttCalls)
	}
	req, _ := http.NewRequest("GET", ts.URL+fmt.Sprintf("/api/v1/sessions/%d/events", sessionID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	defer resp.Body.Close()
	var events []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&events)
	kinds := map[string]bool{}
	for _, ev := range events {
		kinds[fmt.Sprint(ev["kind"])] = true
	}
	if !kinds["user_utterance"] || !kinds["ai_utterance"] {
		t.Fatalf("события: %v", kinds)
	}
	_ = e
}

// TestWSVoiceTTSHeaderFormat — кадры TTS: 4-байтный заголовок {seq,flags},
// seq растёт, у последнего флага bit0.
func TestWSVoiceTTSHeaderFormat(t *testing.T) {
	ts, token, sessionID, m, _ := newVoiceEnv(t)
	conn := dialWS(t, ts, token, sessionID)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	// Триггер: текстовая реплика (короче голосового пути, тот же TTS-код).
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	_ = conn.Write(ctx, websocket.MessageText, mustJSON(
		map[string]any{"type": "ui", "name": "utterance", "payload": map[string]string{"text": "Привет"}}))
	cancel()

	// Ждём бинарные кадры (первый bin — первый кадр ответа).
	deadline := time.Now().Add(5 * time.Second)
	var seqs []int
	var endFlagSeen, binSeen bool
	for time.Now().Before(deadline) && !endFlagSeen {
		c, c2 := context.WithTimeout(context.Background(), time.Until(deadline))
		typ, data, err := conn.Read(c)
		c2()
		if err != nil {
			break
		}
		if typ != websocket.MessageBinary || len(data) < 4 {
			continue
		}
		binSeen = true
		seq := int(binary.LittleEndian.Uint16(data[0:2]))
		flags := binary.LittleEndian.Uint16(data[2:4])
		seqs = append(seqs, seq)
		if flags&0x01 != 0 {
			endFlagSeen = true
		}
	}
	if !binSeen {
		t.Fatal("бинарные кадры TTS не пришли")
	}
	if !endFlagSeen {
		t.Fatalf("нет кадра с финальным флагом (seqs=%v)", seqs)
	}
	if len(seqs) < 2 {
		t.Fatalf("ожидалось >=2 кадров (PCM %d байт), получено %d", m.ttsPCMSize, len(seqs))
	}
	for i, q := range seqs {
		if i == 0 && q != 0 {
			t.Fatalf("первый seq = %d (ожидалось 0)", q)
		}
		if i > 0 && q != seqs[i-1]+1 {
			t.Fatalf("seq не монотонен: %v", seqs)
		}
	}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
