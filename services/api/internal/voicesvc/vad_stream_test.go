package voicesvc

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

// fakeVADVoice — фейковый voice /vad/stream (скриптуется по RMS кадра):
// rms > 1000 — речь (state speech:true при входе); тишина 2 кадра (250 мс)
// → pre_silence; тишина 3 кадра → speech:false. broken — 500 на diale.
type fakeVADVoice struct {
	srv    *httptest.Server
	broken bool
}

func (f *fakeVADVoice) start(t *testing.T) {
	t.Helper()
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f.broken {
			http.Error(w, "vad stream unavailable", http.StatusInternalServerError)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		send := func(v map[string]any) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			_ = conn.Write(ctx, websocket.MessageText, mustJSONVS(v))
			cancel()
		}
		speech, preSent, sil := false, false, 0
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			typ, data, err := conn.Read(ctx)
			cancel()
			if err != nil || typ != websocket.MessageBinary {
				return
			}
			isSpeech := rms16(data) > 1000
			switch {
			case isSpeech && !speech:
				speech, sil, preSent = true, 0, false
				send(map[string]any{"type": "state", "speech": true})
			case !isSpeech && speech:
				sil++
				if sil == 2 && !preSent {
					preSent = true
					send(map[string]any{"type": "state", "speech": true, "pre_silence": true})
				}
				if sil >= 3 {
					speech, sil, preSent = false, 0, false
					send(map[string]any{"type": "state", "speech": false})
				}
			}
		}
	}))
	t.Cleanup(f.srv.Close)
}

func mustJSONVS(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func rms16(pcm []byte) float64 {
	if len(pcm) < 2 {
		return 0
	}
	var sum float64
	for i := 0; i+1 < len(pcm); i += 2 {
		s := float64(int16(binary.LittleEndian.Uint16(pcm[i:])))
		sum += s * s
	}
	return math.Sqrt(sum / float64(len(pcm)/2))
}

// toneVS / silenceVS — кадры 250 мс PCM16 16 кГц (amp 5000 / 10).
func toneVS(ms, amp int) []byte {
	n := 16000 * ms / 1000
	pcm := make([]byte, n*2)
	for i := 0; i < n; i++ {
		v := int16(float64(amp) * math.Sin(2*math.Pi*440*float64(i)/16000))
		pcm[2*i] = byte(v)
		pcm[2*i+1] = byte(v >> 8)
	}
	return pcm
}

func silenceVS(ms int) []byte { return toneVS(ms, 10) }

type eventRecorder struct {
	mu     sync.Mutex
	events []VADEvent
}

func newEventRecorder() *eventRecorder { return &eventRecorder{} }

func (e *eventRecorder) onEvent(ev VADEvent) {
	e.mu.Lock()
	e.events = append(e.events, ev)
	e.mu.Unlock()
}

// snapshot — копия накопленных событий.
func (e *eventRecorder) snapshot() []VADEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]VADEvent(nil), e.events...)
}

// wait — события (≥1) или таймаут.
func (e *eventRecorder) wait(timeout time.Duration) []VADEvent {
	deadline := time.Now().Add(timeout)
	for {
		if evs := e.snapshot(); len(evs) > 0 {
			return evs
		}
		if time.Now().After(deadline) {
			return e.snapshot()
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestVADStreamEvents — полный цикл: pre-roll (тишина → кольцо) → речь →
// pre_silence (копия буфера) → utterance (полная реплика, SpeechMS); хвостовая
// тишина после pre_silence в буфер НЕ входит (буфер == снимку pre_silence).
func TestVADStreamEvents(t *testing.T) {
	f := &fakeVADVoice{}
	f.start(t)
	rec := newEventRecorder()
	s := NewVADStream(f.srv.URL, rec.onEvent)
	defer s.Close()

	time.Sleep(50 * time.Millisecond) // дождаться diale
	send := func(pcm []byte) {
		if !s.Send(pcm) {
			t.Fatal("Send вернул false")
		}
		time.Sleep(20 * time.Millisecond) // ускоренное «реальное время»
	}
	// 4 кадра тишины → pre-roll кольцо (последние 4).
	for i := 0; i < 4; i++ {
		send(silenceVS(250))
	}
	// Речь 2 кадра (500 мс) + тишина 3 кадра (pre_silence на 2-м, end на 3-м).
	for i := 0; i < 2; i++ {
		send(toneVS(250, 5000))
	}
	for i := 0; i < 3; i++ {
		send(silenceVS(250))
	}

	// Ждём 3 события.
	deadline := time.Now().Add(5 * time.Second)
	var evs []VADEvent
	for time.Now().Before(deadline) && len(evs) < 3 {
		evs = rec.snapshot()
		if len(evs) < 3 {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if len(evs) != 3 {
		t.Fatalf("событий: %d (want 3): %+v", len(evs), evs)
	}
	if evs[0].Type != "speech_start" {
		t.Fatalf("событие 1: %q (want speech_start)", evs[0].Type)
	}
	if evs[1].Type != "pre_silence" || len(evs[1].Utterance) == 0 {
		t.Fatalf("событие 2: %+v (want pre_silence с буфером)", evs[1])
	}
	if evs[2].Type != "utterance" || len(evs[2].Utterance) == 0 {
		t.Fatalf("событие 3: %+v (want utterance с репликой)", evs[2])
	}
	// Pre-roll: реплика начинается с тише-кадра (кольцо), не с тона.
	u := evs[2].Utterance
	if rms16(u[:256]) > 1000 {
		t.Fatalf("pre-roll потерян: реплика начинается с тона (rms %.0f)", rms16(u[:256]))
	}
	// Длительность ≥ pre-roll (1 с) + 2 кадра речи (500 мс) ≈ ≥ 1.5 с.
	if evs[2].SpeechMS < 1500 {
		t.Fatalf("SpeechMS = %d, want ≥ 1500 (pre-roll + 2 кадра речи)", evs[2].SpeechMS)
	}
	// Хвостовая тишина после pre_silence не буферизована: буфер == снимку.
	if len(u) != len(evs[1].Utterance) {
		t.Fatalf("буфер расширился после pre_silence: %d → %d", len(evs[1].Utterance), len(u))
	}
}

// TestVADStreamUnavailable — voice недоступен (500): после реконнектов (≤3,
// 500 мс) — событие unavailable (однократно), далее Send бросает (false).
func TestVADStreamUnavailable(t *testing.T) {
	f := &fakeVADVoice{broken: true}
	f.start(t)
	rec := newEventRecorder()
	s := NewVADStream(f.srv.URL, rec.onEvent)
	defer s.Close()

	s.Send(toneVS(250, 5000)) // кадр в буфер sendCh (voice не подключён)
	evs := rec.wait(6 * time.Second)
	if len(evs) != 1 || evs[0].Type != "unavailable" {
		t.Fatalf("события: %+v (want [unavailable])", evs)
	}
	if s.Send(toneVS(250, 5000)) {
		t.Fatal("Send после unavailable должен возвращать false")
	}
	// Дубль unavailable не приходит.
	time.Sleep(300 * time.Millisecond)
	if got := len(rec.snapshot()); got != 1 {
		t.Fatalf("дубль unavailable: событий = %d (want 1)", got)
	}
}
