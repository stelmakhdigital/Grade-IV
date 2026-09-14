package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/auth"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/interviewer"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/session"
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
	// Стартовое сообщение: текущая стадия.
	s.engine.SendTo(id, map[string]any{"type": "stage", "name": snap.Stage, "task": nil})
	s.log.Info("ws: клиент подключился", "session", id, "user", userID, "stage", snap.Stage)

	vadCfg := vad.DefaultConfig()
	vadCfg.EndSilenceMS = s.cfg.VADEndSilenceMS
	vadCfg.RMSThreshold = s.cfg.VADRMSThreshold
	vadCfg.PreSilenceMS = s.cfg.VADPreSilenceMS
	ws := &wsSession{id: id, ctx: ctx, vad: vad.New(vadCfg)}
	ws.touch()

	// Первая реплика ИИ: кандидат ещё не говорил и ИИ не приветствовал (ai_utterance).
	// session_created/события движка в списке — есть всегда, поэтому смотрим транскрипт.
	if snap.Stage == models.StageVoice && !hasAIUtterance(ctx, s.sessions, id) {
		text, err := s.interviewer.OnStageChanged(ctx, id,
			"Кандидат только что начал интервью. Кратко поприветствуй и задай первый вопрос.")
		s.sendInterviewerText(ctx, id, text, err)
		if err == nil {
			s.streamAIAudio(ws, text) // приветствие озвучивается (TTS)
		}
	}
	go s.nudgeLoop(ws, ctx) // решение #7: ИИ заполняет долгие паузы

	s.readLoop(id, conn, ctx, ws)

	s.engine.Detach(id) // FR-S7: обрыв/отключение → пауза
	s.log.Info("ws: клиент отключился", "session", id)
}

// wsSession — состояние WS-соединения сессии: nudge по молчанию, VAD и
// голосовой пайплайн (ADR-002). Пишет в conn только через engine (одно писательство).
type wsSession struct {
	id         int64
	ctx        context.Context // контекст соединения (отмена при обрыве)
	lastActive int64           // unixnano
	lastPCM    int64           // unixnano последнего PCM-кадра от микрофона

	// nudge-лимиты: не чаще раза в 15 с, максимум 3 подряд (сброс при
	// активности кандидата) — без лимита бесконечный поток nudge затапливает
	// контекст LLM и UI («быстро сменяющийся текст»).
	lastNudgeAt atomic.Int64 // unixnano
	nudgeCount  atomic.Int32
	vad        *vad.Detector
	busy       atomic.Bool // голосовой ход занят (один параллельный, turn-taking)
	ttsActive  atomic.Bool // ИИ говорит (стрим TTS) — микрофон не слушается
	// Pre-STT (voice_pipeline.go): предварительное распознавание при первой
	// тишине — перекрывает VAD-хвост, экономит время STT.
	preSTTActive atomic.Bool  // pre-STT запущен (на текущий буфер)
	preSTTBytes  atomic.Int64 // длина буфера на момент запуска (валидность)
	preSTTMu     sync.Mutex
	preSTT       *preSTTResult
}

func (w *wsSession) touch() { atomic.StoreInt64(&w.lastActive, time.Now().UnixNano()) }

// lastCandActivity — когда кандидат последний раз был активен (PCM от
// микрофона). Nudge-отсчёт ведётся от НЕЙ, а не от любой активности:
// иначе nudge-цикл сам обновляет «активность» → бесконечный поток nudge
// (текст «быстро сменяется»).
func (w *wsSession) lastCandActivity() int64 {
	if n := atomic.LoadInt64(&w.lastPCM); n > 0 {
		return n
	}
	return atomic.LoadInt64(&w.lastActive)
}

// setPreSTT / takePreSTT — доступ к pre-STT результату (mutex: гоутрутин
// записывает, ходовой конвейер читает).
func (w *wsSession) setPreSTT(p *preSTTResult) {
	w.preSTTMu.Lock()
	defer w.preSTTMu.Unlock()
	w.preSTT = p
}

func (w *wsSession) takePreSTT() *preSTTResult {
	w.preSTTMu.Lock()
	defer w.preSTTMu.Unlock()
	p := w.preSTT
	w.preSTT = nil
	return p
}

func (w *wsSession) silentS() int {
	return int(time.Since(time.Unix(0, w.lastCandActivity())).Seconds())
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
		if ws.busy.Load() || ws.ttsActive.Load() {
			continue // ход/речь ИИ в процессе — nudge не нужен
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

// handleUIEvent — диспетчеризация ui-событий (ARCHITECTURE.md §4.2).
func (s *Server) handleUIEvent(id int64, msg *wsMessage, ws *wsSession) {
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
		// Вход на стадии — первая реплика ИИ (WP-5): текст + озвучка (TTS).
		switch p.Stage {
		case models.StageLiveCode:
			s.enterLiveCode(ctx, id, ws)
		case models.StageDesign:
			text, err := s.interviewer.OnStageChanged(ctx, id,
				"Кандидат переходит к System Design. Представь формат стадии и задай первую задачу на проектирование.")
			s.sendInterviewerText(ctx, id, text, err)
			if err == nil {
				s.streamAIAudio(ws, text) // озвучка формата стадии (TTS)
			}
		}

	case "finish":
		if _, err := s.engine.Finish(id); err != nil {
			s.engine.SendTo(id, wsErr("invalid_state", err.Error()))
		} else {
			s.startReportGeneration(id) // WP-11: отчёт после завершения
		}

	case "code_run_requested", "submit_solution":
		// Фактическое выполнение кода — WP-6 (sandbox); здесь фиксируем событие (история).
		_, _ = s.eventData(ctx, id, "code_run", map[string]any{"ui": msg.Name, "payload": json.RawMessage(msg.Payload)})

	case "whiteboard_saved":
		// Сохранение холста (PUT /whiteboard) — WP-10; событие фиксируем.
		_, _ = s.eventData(ctx, id, "whiteboard_save", map[string]any{"payload": json.RawMessage(msg.Payload)})

	case "utterance":
		// Реплика кандидата (текстовый режим / FR-V8 / тесты; голосовой STT — WP-4).
		var p struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(msg.Payload, &p); err != nil || strings.TrimSpace(p.Text) == "" {
			s.engine.SendTo(id, wsErr("invalid_request", "payload.text обязателен"))
			return
		}
		text := strings.TrimSpace(p.Text)
		// Конвейер хода (WP-5/ADR-002): событие + transcript → LLM → ai_text → TTS.
		s.runCandidateTurn(ws, text)

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

// liveCodeTask — задача Live-Code из банка sandbox (ARCHITECTURE.md §4.4).
type liveCodeTask struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Statement string `json:"statement"`
	// Files — полный набор файлов задачи (включая тесты из банка, §4.4):
	// кандидат видит их в редакторе и сдаёт обратно в /runs.
	Files map[string]string `json:"files,omitempty"`
}

// enterLiveCode — вход на стадию Live-Code (WP-5/6): выбрать задачу из банка,
// отправить её в сообщении stage, прокомментировать ИИ-интервьюер.
func (s *Server) enterLiveCode(ctx context.Context, id int64, ws *wsSession) {
	sess, err := s.sessions.Get(ctx, id)
	if err != nil {
		return
	}
	var task *liveCodeTask
	if tasks, err := s.fetchLiveCodeTasks(ctx, string(sess.Stack), string(sess.Grade)); err == nil && len(tasks) > 0 {
		// MVP: первая подходящая задача (выборка по случайному смещению — бэклог;
		// детерминизм упрощает тесты и воспроизведение).
		task = &tasks[0]
	} else if err != nil {
		s.log.Warn("livecode: банк задач недоступен", "session", id, "err", err)
	}

	if task != nil {
		s.engine.SendTo(id, map[string]any{
			"type": "stage", "name": models.StageLiveCode,
			"task": map[string]any{
				"id": task.ID, "title": task.Title, "statement": task.Statement, "files": task.Files,
			},
		})
	}
	text, err := s.interviewer.OnStageChanged(ctx, id, stageNote(task))
	s.sendInterviewerText(ctx, id, text, err)
	if err == nil {
		s.streamAIAudio(ws, text) // озвучка задачи (TTS)
	}
}

// stageNote — контекст входа на Live-Code для LLM (задача, если выбрана).
func stageNote(task *liveCodeTask) string {
	if task == nil {
		return "Задача ещё не назначена (банк задач недоступен). Спроси, готов ли кандидат, и напомни формат стадии."
	}
	return fmt.Sprintf("Задача: «%s» (id=%s). Условие: %s", task.Title, task.ID, task.Statement)
}

// fetchLiveCodeTasks — GET {SANDBOX_URL}/api/v1/tasks?stack=&grade= (5 с).
func (s *Server) fetchLiveCodeTasks(ctx context.Context, stack, grade string) ([]liveCodeTask, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		s.cfg.SandboxURL+"/api/v1/tasks?stack="+url.QueryEscape(stack)+"&grade="+url.QueryEscape(grade), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sandbox /tasks: HTTP %d", resp.StatusCode)
	}
	var tasks []liveCodeTask
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

// hasAIUtterance — в событиях сессии есть ai_utterance (ИИ уже приветствовал/отвечал).
func hasAIUtterance(ctx context.Context, sessions *db.SessionStore, id int64) bool {
	events, err := sessions.ListEvents(ctx, id)
	if err != nil {
		return true // при ошибке не спамим приветствием
	}
	for _, ev := range events {
		if ev.Kind == "ai_utterance" {
			return true
		}
	}
	return false
}

func stageKnown(s models.Stage) bool {
	switch s {
	case models.StageVoice, models.StageLiveCode, models.StageDesign, models.StageReport:
		return true
	}
	return false
}
