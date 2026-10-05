package voicesvc

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"nhooyr.io/websocket"
)

// StreamEvent — событие стримингового STT voice /stt/stream (ADR-007):
// "state" (Speech true/false — начало/конец реплики VAD), "partial"
// (промежуточный текст), "final" (итоговый текст реплики), "unavailable"
// (voice-сервис недоступен после реконнектов — деградация на batch-путь).
type StreamEvent struct {
	Type       string
	Text       string
	Confidence float64
	Speech     bool
	SpeechMS   int // final: длительность речи по VAD voice, мс (barge-in)
}

// STTStream — WS-клиент voice /stt/stream: шлёт PCM-кадры (16 кГц mono),
// получает state/partial/final. При обрыве реконнектится (≤3 попыток,
// пауза 500 мс); если voice недоступен — событие "unavailable" (однократно),
// далее вызывающий деградирует на batch-путь (/stt).
//
// Конструктор запускает фоновый цикл (goroutine); Close() — остановка.
type STTStream struct {
	url     string
	onEvent func(StreamEvent)
	sendCh  chan []byte
	done    chan struct{}
	cancel  context.CancelFunc
	once    sync.Once
}

// NewSTTStream — создаёт и запускает стрим-клиент (baseURL — корень voice).
func NewSTTStream(baseURL string, onEvent func(StreamEvent)) *STTStream {
	u := strings.Replace(baseURL, "http://", "ws://", 1)
	u = strings.Replace(u, "https://", "wss://", 1)
	ctx, cancel := context.WithCancel(context.Background())
	s := &STTStream{
		url:     u + "/api/v1/stt/stream",
		onEvent: onEvent,
		sendCh:  make(chan []byte, 64), // ~16 с аудио — буфер на случай сетевых задержек
		done:    make(chan struct{}),
		cancel:  cancel,
	}
	go s.run(ctx)
	return s
}

// Send — неблокирующая отправка PCM-кадра; false — буфер полон (кадр потерян).
func (s *STTStream) Send(pcm []byte) bool {
	select {
	case s.sendCh <- pcm:
		return true
	default:
		return false
	}
}

// Close — остановка стрим-клиента (идемпотентно).
func (s *STTStream) Close() {
	s.once.Do(func() {
		s.cancel()
		close(s.done)
	})
}

const (
	streamDialTimeout  = 2 * time.Second
	streamRetryDelay   = 500 * time.Millisecond
	streamMaxAttempts  = 4 // 1 первичный + 3 реконнекта
	streamWriteTimeout = 2 * time.Second
)

func (s *STTStream) run(ctx context.Context) {
	for attempt := 0; ; attempt++ {
		select {
		case <-s.done:
			return
		default:
		}
		if attempt >= streamMaxAttempts {
			s.onEvent(StreamEvent{Type: "unavailable"})
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
func (s *STTStream) serve(ctx context.Context, conn *websocket.Conn) {
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
			Type       string  `json:"type"`
			Text       string  `json:"text"`
			Confidence float64 `json:"confidence"`
			Speech     bool    `json:"speech"`
			SpeechMS   int     `json:"speech_ms"`
		}
		if err := json.Unmarshal(data, &msg); err != nil || msg.Type == "" {
			continue
		}
		s.onEvent(StreamEvent{Type: msg.Type, Text: msg.Text, Confidence: msg.Confidence, Speech: msg.Speech, SpeechMS: msg.SpeechMS})
	}
}
