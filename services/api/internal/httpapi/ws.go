package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/auth"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/session"
	"nhooyr.io/websocket"
)

// wsMessage — сообщение клиента (текстовые кадры, ARCHITECTURE.md §4.2).
// Бинарные кадры — PCM16 16 кГц mono (микрофон), обрабатываются отдельно.
type wsMessage struct {
	Type    string          `json:"type"`
	Name    string          `json:"name"`
	Payload json.RawMessage `json:"payload"`
}

// wsAuthUser — JWT из query-параметра token (браузеры) или заголовка Authorization.
func (s *Server) wsAuthUser(r *http.Request) (int64, error) {
	token := r.URL.Query().Get("token")
	if token == "" {
		h := r.Header.Get("Authorization")
		if strings.HasPrefix(h, "Bearer ") {
			token = strings.TrimPrefix(h, "Bearer ")
		}
	}
	if token == "" {
		return 0, errors.New("нет токена")
	}
	claims, err := auth.ParseToken(s.cfg.JWTSecret, token)
	if err != nil {
		return 0, err
	}
	return claims.UserID, nil
}

// handleSessionWS — GET /ws/session/{id} (протокол ADR-001).
//
// С→К: stage (стартовое), timer (1 раз/5 с — движком), ai_text/transcript/run_result
//
// (появятся с WP-4/5), error.
// К→С: бинарные PCM-кадры (в WP-3 принимаются и передаются в очередь голосового
// оркестратора — пока подсчитываются), текстовые ui-события.
func (s *Server) handleSessionWS(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_session_id", "некорректный идентификатор сессии")
		return
	}
	userID, err := s.wsAuthUser(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "недействительный токен")
		return
	}

	ctx := r.Context()
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	snap, err := s.engine.Attach(ctx, id, userID, conn)
	if err != nil {
		s.wsSendError(id, conn, err)
		return
	}
	// Стартовое сообщение: текущая стадия (task придёт с WP-5/6 — задачи Live-Code).
	s.engine.SendTo(id, map[string]any{"type": "stage", "name": snap.Stage, "task": nil})
	s.log.Info("ws: клиент подключился", "session", id, "user", userID, "stage", snap.Stage)

	s.readLoop(id, conn, ctx)

	s.engine.Detach(id) // FR-S7: обрыв/отключение → пауза
	s.log.Info("ws: клиент отключился", "session", id)
}

// wsSendError — сообщение error по протоколу (с кодом по ошибке).
func (s *Server) wsSendError(id int64, conn *websocket.Conn, err error) {
	code := "internal"
	switch {
	case errors.Is(err, db.ErrSessionNotFound):
		code = "not_found"
	case errors.Is(err, session.ErrSessionEnded):
		code = "session_ended"
	default:
		code = "invalid_state"
	}
	raw, _ := json.Marshal(map[string]any{"type": "error", "code": code, "msg": err.Error()})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = conn.Write(ctx, websocket.MessageText, raw)
	_ = conn.Close(websocket.StatusProtocolError, code)
}

// readLoop — чтение кадров до закрытия соединения или завершения контекста.
func (s *Server) readLoop(id int64, conn *websocket.Conn, ctx context.Context) {
	var pcmFrames, pcmBytes int
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		typ, data, err := conn.Read(ctx)
		if err != nil {
			s.log.Debug("ws: pcm-статистика", "session", id, "frames", pcmFrames, "bytes", pcmBytes)
			return
		}
		switch typ {
		case websocket.MessageBinary:
			// PCM16 16 кГц mono (ADR-001). Голосовой конвейер (STT/LLM/TTS)
			// подключается в WP-4/5; в WP-3 кадры принимаются (учёт для метрик).
			pcmFrames++
			pcmBytes += len(data)
			s.log.Debug("ws: pcm-кадр", "session", id, "bytes", len(data), "frames", pcmFrames)
		case websocket.MessageText:
			var msg wsMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				s.engine.SendTo(id, map[string]any{
					"type": "error", "code": "invalid_request", "msg": "ожидался JSON-текст {type,name,payload}",
				})
				continue
			}
			if msg.Type != "" && msg.Type != "ui" {
				s.engine.SendTo(id, map[string]any{
					"type": "error", "code": "invalid_request", "msg": "неизвестный type: " + msg.Type,
				})
				continue
			}
			s.handleUIEvent(id, &msg)
		default: // ping/pong — обрабатывает библиотека
		}
	}
}

// handleUIEvent — диспетчеризация ui-событий (ARCHITECTURE.md §4.2).
func (s *Server) handleUIEvent(id int64, msg *wsMessage) {
	ctx := context.Background()
	switch msg.Name {
	case "stage_action":
		var p struct {
			Stage models.Stage `json:"stage"`
		}
		if err := json.Unmarshal(msg.Payload, &p); err != nil || !stageKnown(p.Stage) {
			s.engine.SendTo(id, wsErr("invalid_request", "payload.stage: voice|livecode|design|report"))
			return
		}
		// Сообщение stage (и timer/stage report при финализации) отправляет движок.
		if _, err := s.engine.Transition(id, p.Stage); err != nil {
			s.engine.SendTo(id, wsErr("invalid_state", err.Error()))
			return
		}

	case "finish":
		if _, err := s.engine.Finish(id); err != nil {
			s.engine.SendTo(id, wsErr("invalid_state", err.Error()))
		}

	case "code_run_requested", "submit_solution":
		// Фактическое выполнение кода — WP-6 (sandbox); здесь фиксируем событие (история).
		_, _ = s.eventData(ctx, id, "code_run", map[string]any{"ui": msg.Name, "payload": json.RawMessage(msg.Payload)})

	case "whiteboard_saved":
		// Сохранение холста (PUT /whiteboard) — WP-10; событие фиксируем.
		_, _ = s.eventData(ctx, id, "whiteboard_save", map[string]any{"payload": json.RawMessage(msg.Payload)})

	case "utterance":
		// Текстовая реплика кандидата (деградация в текстовый режим, FR-V8 / тесты).
		var p struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(msg.Payload, &p); err != nil || strings.TrimSpace(p.Text) == "" {
			s.engine.SendTo(id, wsErr("invalid_request", "payload.text обязателен"))
			return
		}
		_, _ = s.eventData(ctx, id, "user_utterance", map[string]any{"text": strings.TrimSpace(p.Text)})

	default:
		s.engine.SendTo(id, wsErr("unknown_ui_event", "неизвестное событие: "+msg.Name))
	}
}

// eventData — событие сессии (data: произвольный JSON).
func (s *Server) eventData(ctx context.Context, sessionID int64, kind string, data map[string]any) (int, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return 0, err
	}
	return s.sessions.AddEvent(ctx, sessionID, kind, raw)
}

func wsErr(code, msg string) map[string]any {
	return map[string]any{"type": "error", "code": code, "msg": msg}
}

func stageKnown(s models.Stage) bool {
	switch s {
	case models.StageVoice, models.StageLiveCode, models.StageDesign, models.StageReport:
		return true
	}
	return false
}
