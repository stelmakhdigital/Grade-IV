package voicesvc

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/vad"
	"nhooyr.io/websocket"
)

// VADEvent — событие Silero VAD-стрима voice /api/v1/vad/stream (ADR-002,
// поправка 2026-10-09, вариант 2): "speech_start" (старт речи), "pre_silence"
// (предварительная тишина в речи — Utterance = текущий буфер, сигнал pre-STT),
// "utterance" (реплика завершена: Utterance = полная реплика с pre-roll,
// SpeechMS = длительность всего буфера (с pre-roll), мс; PreRollMS = доля
// pre-roll в буфере, мс — для вычитания при barge-in пороге), "unavailable"
// (voice недоступен после реконнектов — деградация на energy-путь).
type VADEvent struct {
	Type      string // "speech_start" | "pre_silence" | "utterance" | "unavailable"
	Utterance []byte // pre_silence: текущий буфер; utterance: полная реплика (с pre-roll)
	SpeechMS  int    // utterance: длительность всего буфера (с pre-roll), мс
	PreRollMS int    // utterance: длительность pre-roll (тишина перед речью), мс
}

// vadPreRollFrames — pre-roll кольцо: последние кадры тишины перед словом
// (~1 с @ 250 мс/кадр) — чистый старт слова в буфере реплики.
const vadPreRollFrames = 4

// vadSpeechGateRMS — после pre_silence тише-кадры НЕ добавляются в буфер
// (порог «тихо», как RMSThreshold energy-детектора): буфер остаётся
// идентичным снимку pre_silence (pre-STT валиден), возобновлённая речь
// (RMS выше) расширяет буфер — pre-STT инвалидируется, STT перезапустится
// на полной реплике.
const vadSpeechGateRMS = 100

// VADStream — WS-клиент voice /vad/stream (аналог STTStream): шлёт PCM-кадры
// (16 кГц mono), сам аккумулирует буфер реплики (pre-roll кольцо при тишине,
// буфер при речи), эмитит speech_start/pre_silence/utterance. При обрыве —
// реконнект (≤3 попыток, пауза 500 мс); если voice недоступен — "unavailable"
// (однократно), далее Send бросает кадры (false).
//
// Конструктор запускает фоновый цикл (goroutine); Close() — остановка.
type VADStream struct {
	url     string
	onEvent func(VADEvent)
	sendCh  chan []byte
	done    chan struct{}
	cancel  context.CancelFunc
	once    sync.Once
	dead    atomic.Bool // voice недоступен (unavailable) — Send бросает

	// Буферы реплики (writer: Send; reader: state-события) — под mu.
	mu         sync.Mutex
	ring       [][]byte // pre-roll: последние кадры (не-речь), ≤ vadPreRollFrames
	buf        []byte   // аудио текущей реплики (от pre-roll)
	prerollB   int      // байты pre-roll, включённые в буфер при старте речи (для PreRollMS)
	preEmitted bool     // pre_silence уже выдан (тише-кадры не буферизуем)
	inSpeech   atomic.Bool
}

// NewVADStream — создаёт и запускает VAD-стрим-клиент (baseURL — корень voice).
func NewVADStream(baseURL string, onEvent func(VADEvent)) *VADStream {
	u := strings.Replace(baseURL, "http://", "ws://", 1)
	u = strings.Replace(u, "https://", "wss://", 1)
	ctx, cancel := context.WithCancel(context.Background())
	s := &VADStream{
		url:     u + "/api/v1/vad/stream",
		onEvent: onEvent,
		sendCh:  make(chan []byte, 64), // ~16 с аудио — буфер на сетевые задержки
		done:    make(chan struct{}),
		cancel:  cancel,
	}
	go s.run(ctx)
	return s
}

// Send — неблокирующая отправка PCM-кадра + локальная буферизация:
// pre-roll кольцо (не-речь, ≤ vadPreRollFrames) / буфер реплики (речь).
// false — voice недоступен или буфер отправки полон (кадр потерян).
func (s *VADStream) Send(pcm []byte) bool {
	if s.dead.Load() {
		return false
	}
	s.mu.Lock()
	if s.inSpeech.Load() {
		if !(s.preEmitted && vad.RMSPCM16(pcm) < vadSpeechGateRMS) {
			s.buf = append(s.buf, pcm...)
		}
	} else {
		s.ring = append(s.ring, pcm)
		if len(s.ring) > vadPreRollFrames {
			s.ring = s.ring[1:]
		}
	}
	s.mu.Unlock()
	select {
	case s.sendCh <- pcm:
		return true
	default:
		return false
	}
}

// Close — остановка стрим-клиента (идемпотентно).
func (s *VADStream) Close() {
	s.once.Do(func() {
		s.cancel()
		close(s.done)
	})
}

func (s *VADStream) run(ctx context.Context) {
	for attempt := 0; ; attempt++ {
		select {
		case <-s.done:
			return
		default:
		}
		if attempt >= streamMaxAttempts {
			s.dead.Store(true)
			s.onEvent(VADEvent{Type: "unavailable"}) // однократно: цикл завершён
			return
		}
		if attempt > 0 {
			select {
			case <-s.done:
				return
			case <-time.After(streamRetryDelay):
			}
		}
		dctx, cancel := context.WithTimeout(ctx, streamDialTimeout)
		conn, _, err := websocket.Dial(dctx, s.url, nil)
		cancel()
		if err != nil {
			continue
		}
		s.serve(ctx, conn)
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}
}

// serve — писатель (sendCh → conn) + читатель (conn → onEvent) до обрыва.
func (s *VADStream) serve(ctx context.Context, conn *websocket.Conn) {
	go func() {
		for {
			select {
			case <-s.done:
				_ = conn.Close(websocket.StatusNormalClosure, "")
				return
			case pcm := <-s.sendCh:
				wctx, wc := context.WithTimeout(ctx, streamWriteTimeout)
				err := conn.Write(wctx, websocket.MessageBinary, pcm)
				wc()
				if err != nil {
					_ = conn.Close(websocket.StatusGoingAway, "")
					return
				}
			}
		}
	}()
	for {
		select {
		case <-s.done:
			return
		default:
		}
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		var msg struct {
			Type       string `json:"type"`
			Speech     bool   `json:"speech"`
			PreSilence bool   `json:"pre_silence"`
		}
		if err := json.Unmarshal(data, &msg); err != nil || msg.Type != "state" {
			continue
		}
		switch {
		case msg.Speech && !msg.PreSilence:
			// Старт речи: сброс буфера, pre-roll кольцо — в начало буфера.
			s.mu.Lock()
			s.buf = nil
			s.prerollB = 0
			for _, f := range s.ring {
				s.buf = append(s.buf, f...)
				s.prerollB += len(f)
			}
			s.ring = nil
			s.preEmitted = false
			s.inSpeech.Store(true)
			s.mu.Unlock()
			s.onEvent(VADEvent{Type: "speech_start"})
		case msg.Speech && msg.PreSilence:
			// Pre-silence: КОПИЯ текущего буфера (для pre-STT).
			s.mu.Lock()
			snap := make([]byte, len(s.buf))
			copy(snap, s.buf)
			s.preEmitted = true
			s.mu.Unlock()
			s.onEvent(VADEvent{Type: "pre_silence", Utterance: snap})
		case !msg.Speech:
			// Конец реплики: полный буфер + длительность.
			s.mu.Lock()
			u := s.buf
			s.buf = nil
			s.ring = nil
			s.preEmitted = false
			s.inSpeech.Store(false)
			s.mu.Unlock()
			if len(u) > 0 {
				ms := int(int64(len(u)) * 1000 / 2 / SampleRate)
				prerollMS := int(int64(s.prerollB) * 1000 / 2 / SampleRate)
				s.prerollB = 0
				s.onEvent(VADEvent{Type: "utterance", Utterance: u, SpeechMS: ms, PreRollMS: prerollMS})
			}
		}
	}
}
