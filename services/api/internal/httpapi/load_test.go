package httpapi

// Фаза 4 (TEST_PLAN §5): load-тест real-time нагрузки — 50 одновременных
// WS-голосовых сессий (LLM-мок; voice-сервис не используется — STT-ошибки
// глотаются, ходы не строятся; нагрузка: WS-транспорт, VAD, движок сессий,
// таймеры, nudge-циклы). Контракт: все сессии живы до конца, таймер тикает
// (1 tick / 5 с) без срывов, клиентских error-сообщений нет.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"nhooyr.io/websocket"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/config"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/llm"
)

const (
	loadSessions = 50
	loadDuration = 20 * time.Second
	loadChunk    = 8000 // 250 мс @ 16 кГц PCM16
)

// loadChunkPCM — синус 440 Гц, амплитуда 8000 (RMS >> порога 500): VAD
// реально обрабатывает кадры, но реплика не завершается (нет тишины) —
// непрерывная «речь» как у живого кандидата.
func loadChunkPCM() []byte {
	buf := make([]byte, loadChunk)
	for i := 0; i < loadChunk/2; i++ {
		s := int16(8000 * math.Sin(2*math.Pi*440*float64(i)/16000))
		buf[2*i] = byte(s)
		buf[2*i+1] = byte(s >> 8)
	}
	return buf
}

func TestLoadWS50Sessions(t *testing.T) {
	// Свой env (не newTestEnv): нужен доступ к Engine().Run — в проде тик-цикл
	// запускает cmd/api, а timer-сообщения приходят только от него.
	cfg := &config.Config{
		Addr: ":0", DatabaseURL: "sqlite://:memory:", JWTSecret: "test-secret",
		JWTExpiryHours: 1, MinutesFreeS: 3600,
	}
	database, dialect, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.Migrate(t.Context(), database, dialect); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	srv := NewWithLLM(cfg, database, dialect, slog.New(slog.DiscardHandler), llm.NewMockProvider())
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	go srv.Engine().Run(ctx)
	t.Cleanup(srv.Engine().Stop)

	type sessMetrics struct {
		timers  int // текстовые сообщения типа timer
		msgs    int
		errs    int
		lastErr string
	}
	metrics := make([]sessMetrics, loadSessions)

	var wg sync.WaitGroup
	for i := 0; i < loadSessions; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			m := &metrics[n]

			// регистрация + сессия (doJSON отдаёт body map)
			code, regBody := doJSON(t, "POST", ts.URL+"/api/v1/auth/register",
				map[string]string{"email": fmt.Sprintf("load-%d@example.com", n), "password": "password1"},
				nil)
			if code != http.StatusCreated {
				t.Errorf("load[%d]: register: %d %v", n, code, regBody)
				return
			}
			tok, _ := regBody["token"].(string)
			if tok == "" {
				t.Errorf("load[%d]: нет token: %v", n, regBody)
				return
			}
			code, body := doJSON(t, "POST", ts.URL+"/api/v1/sessions",
				map[string]string{"grade": "middle", "stack": "go"},
				map[string]string{"Authorization": "Bearer " + tok})
			if code != http.StatusCreated {
				t.Errorf("load[%d]: sessions: %d %v", n, code, body)
				return
			}
			sid, _ := body["id"].(float64)
			if sid == 0 {
				t.Errorf("load[%d]: нет id сессии: %v", n, body)
				return
			}

			// WS
			base := ts.URL
			wsBase := base
			if strings.HasPrefix(wsBase, "http://") {
				wsBase = "ws://" + strings.TrimPrefix(wsBase, "http://")
			}
			u, _ := url.ParseRequestURI(wsBase + "/ws/session/" + fmt.Sprint(sid))
			q := u.Query()
			q.Set("token", tok)
			u.RawQuery = q.Encode()
			conn, _, err := websocket.Dial(ctx, u.String(), &websocket.DialOptions{
				HTTPClient: http.DefaultClient,
			})
			if err != nil {
				t.Errorf("load[%d]: dial: %v", n, err)
				return
			}
			defer conn.CloseNow()

			deadline := time.Now().Add(loadDuration)
			tick := time.NewTicker(250 * time.Millisecond)
			defer tick.Stop()
			chunk := loadChunkPCM()

			recv := make(chan [2]any, 64) // (тип, данные)
			errs := make(chan error, 1)
			go func() {
				for {
					typ, data, err := conn.Read(ctx)
					if err != nil {
						errs <- err
						return
					}
					recv <- [2]any{typ, data}
				}
			}()

			for time.Now().Before(deadline) {
				select {
				case <-tick.C:
					if err := conn.Write(ctx, websocket.MessageBinary, chunk); err != nil {
						t.Errorf("load[%d]: write: %v", n, err)
						return
					}
				case msg := <-recv:
					if msg[0] == websocket.MessageText {
						m.msgs++
						var d struct {
							Type string `json:"type"`
							Text string `json:"text"`
						}
						_ = json.Unmarshal(msg[1].([]byte), &d)
						switch d.Type {
						case "timer":
							m.timers++
						case "error":
							m.errs++
							m.lastErr = d.Text
						}
					}
				case err := <-errs:
					t.Errorf("load[%d]: read: %v (err-сообщений до срыва: %d, last: %s)",
						n, err, m.errs, m.lastErr)
					return
				}
			}
		}(i)
	}
	wg.Wait()

	tickSum, msgSum, errSum, bad := 0, 0, 0, 0
	for i, m := range metrics {
		tickSum += m.timers
		msgSum += m.msgs
		errSum += m.errs
		// 20 с / 5 с = ~4 тика (+стартовый); допускаем 3..7.
		if m.timers < 3 || m.timers > 7 {
			bad++
			t.Logf("load[%d]: тиков таймера=%d (ожидается 3..7), msgs=%d errs=%d", i, m.timers, m.msgs, m.errs)
		}
		if m.errs > 0 {
			bad++
			t.Logf("load[%d]: клиентских ошибок=%d: %s", i, m.errs, m.lastErr)
		}
	}
	t.Logf("LOAD: сессий=%d ticks=%d msgs=%d errs=%d", loadSessions, tickSum, msgSum, errSum)
	if bad > 0 {
		t.Errorf("load: %d сессий с отклонением (тики 3..7, 0 ошибок)", bad)
	}
}
