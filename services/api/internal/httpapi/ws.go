package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/auth"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/interviewer"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/vad"

	"nhooyr.io/websocket"
)

// wsMessage — сообщение клиента (текстовые кадры, ARCHITECTURE.md §4.2).
// Бинарные кадры — PCM16 16 кГц mono (микрофон), обрабатываются отдельно.
type wsMessage struct {
	Type    string          `json:"type"`
	Name    string          `json:"name"`
	Payload json.RawMessage `json:"payload"`
}

// wsAcceptOptions — проверка origin выключена осознанно. Прод идёт через
// reverse-proxy (Caddy → nginx → api), а nginx отдаёт Host без порта
// (proxy_set_header Host $host): браузерный Origin «http://host:8081» не
// совпадал с «host», апгрейд отваливался с 403 — голосовая стадия не
// начиналась, в логе не было ни строки. Без проверки безопасно: апгрейд
// авторизуется одноразовым тикетом (или JWT в заголовке для не-браузерных
// клиентов), cookie api не раздаёт, поэтому межсайтовой запрос чужую сессию не подхватит.
var wsAcceptOptions = &websocket.AcceptOptions{InsecureSkipVerify: true}

// wsAuthUser — аутентификация WS-upgrade: одноразовый тикет (spec-14/#12) либо
// JWT в заголовке Authorization (не-браузерные клиенты, тесты).
//
// JWT из query больше не принимается совсем: токен в URL оседает в access-логе
// caddy/nginx (пишется полный URL) и в истории браузера, а живёт он до конца
// TTL — то есть любой, кто читает логи, получает чужой аккаунт. Тикет ровно для
// этого и вводили, а fallback на «?token=» обходилась первая же сетевая ошибка.
func (s *Server) wsAuthUser(r *http.Request) (int64, error) {
	if ticket := r.URL.Query().Get("ticket"); ticket != "" {
		if id, ok := s.wsTickets.consume(ticket); ok {
			return id, nil
		}
		return 0, errors.New("тикет недействителен или уже использован")
	}
	token := ""
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		token = strings.TrimPrefix(h, "Bearer ")
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
	id, ok := parseSessionID(w, r)
	if !ok {
		return
	}
	userID, err := s.wsAuthUser(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "недействительный токен")
		return
	}

	ctx := r.Context()
	conn, err := websocket.Accept(w, r, wsAcceptOptions)
	if err != nil {
		s.log.Warn("ws: апгрейд отклонён", "session", id, "err", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	snap, err := s.engine.Attach(ctx, id, userID, conn)
	if err != nil {
		s.wsSendError(id, conn, err)
		return
	}
	// Стартовое сообщение: текущая стадия.
	// spec-18: ResendStage — та же сборка, что и в Transition (board для design).
	s.engine.ResendStage(id)
	if snap.Stage == models.StageLiveCode {
		s.resendLiveCodeTasks(ctx, id)
	}
	s.log.Info("ws: клиент подключился", "session", id, "user", userID, "stage", snap.Stage)

	vadCfg := vad.DefaultConfig()
	vadCfg.EndSilenceMS = s.cfg.VADEndSilenceMS
	vadCfg.RMSThreshold = s.cfg.VADRMSThreshold
	vadCfg.PreSilenceMS = s.cfg.VADPreSilenceMS
	ws := &wsSession{id: id, ctx: ctx, vad: vad.New(vadCfg)}
	// F1 (Итерация 10): голос интервьюера (выбран в кабинете, сохранён в сессии)
	// — в контекст TTS (Ctx() оборачивает; Client.TTS кладёт в запрос "speaker").
	// Пусто — дефолт voice-сервиса (ru_01 → xenia), backward-compat.
	if sess, err := s.sessions.Get(ctx, id); err == nil && sess.TTSSpeaker != "" {
		ws.speaker = sess.TTSSpeaker
	}
	ws.touch()
	s.wsRegister(id, ws) // реестр: pause остановит активный TTS-стрим (FR-S7)
	defer s.wsUnregister(id)

	// Первая реплика ИИ: кандидат ещё не говорил и ИИ не приветствовал (ai_utterance).
	// session_created/события движка в списке — есть всегда, поэтому смотрим транскрипт.
	if snap.Stage == models.StageVoice && !hasAIUtterance(ctx, s.sessions, id) {
		text, err := s.interviewer.OnStageChanged(ctx, id,
			"Кандидат только что начал интервью. Кратко поприветствуй и задай первый вопрос.")
		s.speak(ctx, id, ws, text, err) // приветствие озвучивается (TTS)
	}
	go s.nudgeLoop(ws, ctx) // решение #7: ИИ заполняет долгие паузы

	s.readLoop(id, conn, ctx, ws)

	ws.closeSTTStream() // ADR-007: останавливаем voice /stt/stream
	ws.closeVADStream() // ADR-002 (2026-10-09): voice /vad/stream
	s.engine.Detach(id) // FR-S7: обрыв/отключение → пауза
	s.log.Info("ws: клиент отключился", "session", id)
}

// nudgeLoop — раз в секунду: если сессия активна, стадия voice и кандидат молчит
// больше SILENCE_NUDGE_S — nudge-ход (решение #7). Пишет в conn только через
// engine.SendTo (правило единственного писателя).
func (s *Server) nudgeLoop(ws *wsSession, ctx context.Context) {
	if s.cfg.SilenceNudgeS <= 0 {
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		snap, err := s.engine.Snapshot(ws.id)
		if err != nil || snap.Status != models.StatusActive || snap.Stage != models.StageVoice {
			continue
		}
		if ws.busy.Load() || ws.ttsActive.Load() || ws.isSpeaking() {
			continue // ход/речь ИИ/речь кандидата (в т.ч. Silero-путь) — nudge не нужен
		}
		if ws.recording.Load() {
			continue // режим записи (FR-S8): длинные паузы внутри записи — норма,
			// nudge не прерывает запись (ответ ИИ — только по явной отправке)
		}
		silent := ws.silentS()
		if silent < s.cfg.SilenceNudgeS {
			ws.nudgeCount.Store(0) // кандидат активен — сброс счётчика
			continue
		}
		if ws.nudgeCount.Load() >= 3 {
			continue // лимит: ждём кандидата без новых реплик
		}
		if time.Since(time.Unix(0, ws.lastNudgeAt.Load())) < 15*time.Second {
			continue // cooldown между nudge
		}
		ws.nudgeCount.Add(1)
		text, err := s.interviewer.Nudge(ctx, ws.id, silent)
		if err != nil {
			s.log.Debug("nudge: ошибка LLM", "session", ws.id, "err", err)
			continue
		}
		// Nudge НЕ обновляет lastCandActivity (это не активность кандидата):
		// иначе отсчёт тишины обнуляется и nudge-поток становится бесконечным.
		ws.lastNudgeAt.Store(time.Now().UnixNano())
		s.engine.SendTo(ws.id, map[string]any{"type": "ai_text", "text": text})
		s.streamAIAudio(ws, text) // озвучка подсказки (FR-S3: пауза заполняется голосом)
	}
}

// sendInterviewerText — ai_text по WS или стандартный fallback при ошибке LLM.
func (s *Server) sendInterviewerText(ctx context.Context, id int64, result string, err error) {
	if err != nil {
		s.log.Warn("interviewer: LLM-ход не удался", "session", id, "err", err)
		s.engine.SendTo(id, map[string]any{"type": "ai_text", "text": interviewer.FallbackText})
		return
	}
	s.engine.SendTo(id, map[string]any{"type": "ai_text", "text": result})
}

// speak — ai_text по WS (fallback при ошибке LLM) + озвучка (TTS) при успехе.
func (s *Server) speak(ctx context.Context, id int64, ws *wsSession, text string, err error) {
	s.sendInterviewerText(ctx, id, text, err)
	if err == nil {
		s.streamAIAudio(ws, text)
	}
}

// wsSendError — сообщение error по протоколу. Наружу уходит код и человекочитаемое
// сообщение из clientError; внутренности ошибки (transport, пути, SQL) — в лог.
func (s *Server) wsSendError(id int64, conn *websocket.Conn, err error) {
	code, msg := clientError(err)
	s.log.Warn("ws: ошибка сессии", "session", id, "code", code, "err", err)
	raw, _ := json.Marshal(map[string]any{"type": "error", "code": code, "msg": msg})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = conn.Write(ctx, websocket.MessageText, raw)
	_ = conn.Close(websocket.StatusProtocolError, code)
}

// readLoop — чтение кадров до закрытия соединения или завершения контекста.
func (s *Server) readLoop(id int64, conn *websocket.Conn, ctx context.Context, ws *wsSession) {
	var pcmFrames, pcmBytes int
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		typ, data, err := conn.Read(ctx)
		if err != nil {
			s.log.Debug("ws: pcm-статистика", "session", id, "frames", pcmFrames, "bytes", pcmBytes, "err", err)
			return
		}
		ws.touch() // любая активность сдвигает lastActive
		if typ == websocket.MessageBinary {
			atomic.StoreInt64(&ws.lastPCM, time.Now().UnixNano()) // nudge-отсчёт — от активности кандидата
		}
		switch typ {
		case websocket.MessageBinary:
			// PCM16 16 кГц mono (ADR-001) → VAD → голосовой пайплайн (ADR-002).
			pcmFrames++
			pcmBytes += len(data)
			s.feedVAD(ws, data)
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
			s.handleUIEvent(id, &msg, ws)
		default: // ping/pong — обрабатывает библиотека
		}
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
