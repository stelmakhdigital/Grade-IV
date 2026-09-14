package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/llm"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/session"
)

// whiteboardMaxBodyBytes — лимит тела PUT /whiteboard (state + base64-картинка).
const whiteboardMaxBodyBytes = 8 << 20

// whiteboardMaxPngBytes — лимит PNG схемы (10 МБ; ADR-004: 1920 px+).
const whiteboardMaxPngBytes = 10 << 20

// DTO сессии для REST (ARCHITECTURE.md §4.1).
type sessionDTO struct {
	ID             int64      `json:"id"`
	Grade          string     `json:"grade"`
	Stack          string     `json:"stack"`
	Stage          string     `json:"stage"`
	Status         string     `json:"status"`
	DurationLimitS int        `json:"duration_limit_s"`
	ActiveSeconds  float64    `json:"active_seconds"`
	TimeLeftS      int        `json:"time_left_s"`
	StartedAt      time.Time  `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	PausedAt       *time.Time `json:"paused_at,omitempty"`
}

func toDTO(m models.Session, snap *session.Snapshot) sessionDTO {
	d := sessionDTO{
		ID:             m.ID,
		Grade:          string(m.Grade),
		Stack:          string(m.Stack),
		Stage:          string(m.Stage),
		Status:         string(m.Status),
		DurationLimitS: m.DurationLimitS,
		ActiveSeconds:  m.ActiveSeconds,
		TimeLeftS:      0,
		StartedAt:      m.StartedAt,
		FinishedAt:     m.FinishedAt,
		PausedAt:       m.PausedAt,
	}
	if snap != nil {
		d.Stage = string(snap.Stage)
		d.Status = string(snap.Status)
		d.ActiveSeconds = snap.ActiveSeconds
		d.TimeLeftS = snap.TimeLeftS
	}
	return d
}

// handleSessionsCreate — POST /api/v1/sessions {grade, stack}.
// 201: {id, ws_url, duration_limit_s}; 402 — нет доступных минут (ARCHITECTURE §4.1).
func (s *Server) handleSessionsCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Grade models.Grade `json:"grade"`
		Stack models.Stack `json:"stack"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "ожидается JSON {grade, stack}")
		return
	}
	m, err := s.engine.Create(r.Context(), userIDFromContext(r.Context()), body.Grade, body.Stack)
	switch {
	case err == nil:
		writeJSON(w, http.StatusCreated, map[string]any{
			"id": m.ID, "ws_url": "/ws/session/" + strconv.FormatInt(m.ID, 10),
			"duration_limit_s": m.DurationLimitS,
		})
	case errors.Is(err, session.ErrNoMinutes):
		writeError(w, http.StatusPaymentRequired, "out_of_minutes",
			"нет доступных минут — пополните лимит или дождитесь обновления тарифа")
	case errors.Is(err, session.ErrInvalidParams):
		writeError(w, http.StatusBadRequest, "invalid_request", "некорректный грейд или стек: "+err.Error())
	default:
		s.log.Error("create session", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "не удалось создать сессию")
	}
}

// handleSessionsList — GET /api/v1/sessions.
func (s *Server) handleSessionsList(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r.Context())
	list, err := s.sessions.ListByUser(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "не удалось получить список")
		return
	}
	out := make([]sessionDTO, 0, len(list))
	for _, m := range list {
		out = append(out, toDTO(m, s.engineSnapshot(m.ID)))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSessionGet — GET /api/v1/sessions/{id}.
func (s *Server) handleSessionGet(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSessionID(w, r)
	if !ok {
		return
	}
	userID := userIDFromContext(r.Context())
	m, err := s.sessions.GetOwned(r.Context(), id, userID)
	if errors.Is(err, db.ErrSessionNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "сессия не найдена")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "не удалось получить сессию")
		return
	}
	writeJSON(w, http.StatusOK, toDTO(m, s.engineSnapshot(id)))
}

// handleSessionAction — POST /api/v1/sessions/{id}/{pause|resume|finish}.
func (s *Server) handleSessionAction(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSessionID(w, r)
	if !ok {
		return
	}
	var (
		snap *session.Snapshot
		err  error
	)
	switch r.PathValue("action") {
	case "pause":
		snap, err = s.engine.Pause(id)
	case "resume":
		snap, err = s.engine.Resume(id)
	case "finish":
		snap, err = s.engine.Finish(id)
	default:
		writeError(w, http.StatusNotFound, "not_found", "неизвестное действие")
		return
	}
	if err != nil {
		s.writeSessionEngineError(w, err)
		return
	}
	if r.PathValue("action") == "finish" {
		s.startReportGeneration(id) // WP-11: отчёт после завершения
	}
	m, err := s.sessions.GetOwned(r.Context(), id, userIDFromContext(r.Context()))
	if err != nil {
		// Снимок уже выдан; при ошибке БД отдаём из снимка.
		writeJSON(w, http.StatusOK, sessionDTO{
			ID: id, Stage: string(snap.Stage), Status: string(snap.Status),
			DurationLimitS: snap.DurationLimitS, ActiveSeconds: snap.ActiveSeconds, TimeLeftS: snap.TimeLeftS,
		})
		return
	}
	writeJSON(w, http.StatusOK, toDTO(m, snap))
}

// handleSessionEvents — GET /api/v1/sessions/{id}/events (транскрипт/история, FR-A4).
func (s *Server) handleSessionEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSessionID(w, r)
	if !ok {
		return
	}
	m, err := s.sessions.GetOwned(r.Context(), id, userIDFromContext(r.Context()))
	if errors.Is(err, db.ErrSessionNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "сессия не найдена")
		return
	}
	events, err := s.sessions.ListEvents(r.Context(), m.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "не удалось получить события")
		return
	}
	type eventDTO struct {
		Seq  int             `json:"seq"`
		TS   time.Time       `json:"ts"`
		Kind string          `json:"kind"`
		Data json.RawMessage `json:"data"`
	}
	out := make([]eventDTO, 0, len(events))
	for _, e := range events {
		out = append(out, eventDTO{Seq: e.Seq, TS: e.TS, Kind: e.Kind, Data: json.RawMessage(e.Data)})
	}
	writeJSON(w, http.StatusOK, out)
}

// engineSnapshot — снимок из движка, nil если нет (исторические сессии).
func (s *Server) engineSnapshot(id int64) *session.Snapshot {
	snap, err := s.engine.Snapshot(id)
	if err != nil {
		return nil
	}
	return snap
}

// parseSessionID — {id} из пути; при ошибке пишет ответ и возвращает ok=false.
func parseSessionID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_session_id", "некорректный идентификатор сессии")
		return 0, false
	}
	return id, true
}

// writeSessionEngineError — маппинг ошибок движка на коды HTTP.
func (s *Server) writeSessionEngineError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, db.ErrSessionNotFound):
		writeError(w, http.StatusNotFound, "not_found", "сессия не найдена")
	case errors.Is(err, session.ErrNoMinutes):
		writeError(w, http.StatusPaymentRequired, "out_of_minutes", "нет доступных минут")
	case errors.Is(err, session.ErrSessionEnded),
		errors.Is(err, session.ErrIllegalStage),
		errors.Is(err, session.ErrIllegalStatus),
		errors.Is(err, session.ErrNotAttached),
		errors.Is(err, session.ErrStageNotInScope):
		writeError(w, http.StatusConflict, "invalid_state", err.Error())
	default:
		s.log.Error("session engine", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "внутренняя ошибка")
	}
}

// handleWhiteboardPut — PUT /api/v1/sessions/{id}/whiteboard (ADR-004, WP-10).
// {state (JSON Excalidraw), png? (base64, MVP — не обязателен),
//
//	structure? {blocks: [имена], links: n}} — структура схемы для оценки ИИ.
//
// После сохранения — fire-and-forget ИИ-оценка (ai_text по WS, как code review).
func (s *Server) handleWhiteboardPut(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSessionID(w, r)
	if !ok {
		return
	}
	userID := userIDFromContext(r.Context())
	m, err := s.sessions.GetOwned(r.Context(), id, userID)
	if errors.Is(err, db.ErrSessionNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "сессия не найдена")
		return
	}
	if err != nil {
		s.log.Error("get session", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "не удалось получить сессию")
		return
	}
	if session.IsTerminalStatus(m.Status) {
		writeError(w, http.StatusConflict, "invalid_state", "сессия завершена")
		return
	}

	var body struct {
		State     json.RawMessage `json:"state"`
		Png       string          `json:"png"`
		Structure *struct {
			Blocks []string `json:"blocks"`
			Links  int      `json:"links"`
		} `json:"structure"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, whiteboardMaxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request",
			"ожидается JSON {state, png?, structure?}")
		return
	}
	if len(body.State) == 0 || string(body.State) == "null" {
		writeError(w, http.StatusBadRequest, "invalid_request", "state обязателен (JSON Excalidraw)")
		return
	}

	// Структура (детерминированная часть оценки, ADR-004): блоки и связи.
	blocksJSON := []byte("{}")
	structure := map[string]any{"blocks": []string{}, "links": 0}
	if body.Structure != nil {
		structure = map[string]any{"blocks": body.Structure.Blocks, "links": body.Structure.Links}
	}
	blocksJSON, _ = json.Marshal(structure)

	// PNG схемы (base64 → файл на узле; ADR-004: оценка vision по PNG + структуре).
	var pngBytes []byte
	if body.Png != "" {
		decoded, err := base64.StdEncoding.DecodeString(body.Png)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "png: некорректное base64")
			return
		}
		if len(decoded) > whiteboardMaxPngBytes {
			writeError(w, 413, "too_large", "png слишком большой")
			return
		}
		pngBytes = decoded
	}

	if err := s.whiteboards.Save(r.Context(), id, body.State, blocksJSON, pngBytes); err != nil {
		s.log.Error("whiteboard save", "session", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "ошибка сохранения холста")
		return
	}
	_, _ = s.sessions.AddEvent(r.Context(), id, "whiteboard_save", mustMarshal(structure))
	s.log.Info("whiteboard сохранён", "session", id, "blocks", len(structure["blocks"].([]string)))

	// ИИ-оценка по рубрике (fire-and-forget; транскрипт устного ответа
	// стадии подтягивает interviewer из событий).
	go func() {
		rctx, cancel := context.WithTimeout(context.Background(), llm.DefaultTimeout)
		defer cancel()
		text, err := s.interviewer.OnDesignSubmit(rctx, id, structure, pngBytes)
		if err != nil {
			s.log.Warn("design: ИИ-оценка не удалась", "session", id, "err", err)
			return
		}
		s.engine.SendTo(id, map[string]any{"type": "ai_text", "text": text})
	}()

	writeJSON(w, http.StatusOK, map[string]any{"saved": true, "structure": structure})
}

// handleReportGet — GET /api/v1/sessions/{id}/report (WP-11).
// 200 — отчёт; 202 — сессия завершена, отчёт генерируется; 409 — сессия не завершена.
func (s *Server) handleReportGet(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSessionID(w, r)
	if !ok {
		return
	}
	userID := userIDFromContext(r.Context())
	m, err := s.sessions.GetOwned(r.Context(), id, userID)
	if errors.Is(err, db.ErrSessionNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "сессия не найдена")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "не удалось получить сессию")
		return
	}
	if m.Status != models.StatusFinished && m.Status != models.StatusAborted {
		writeError(w, http.StatusConflict, "invalid_state", "отчёт доступен после завершения сессии")
		return
	}
	overall, gradeRec, criteria, strengths, weaknesses, recommendations, err :=
		s.reports.Get(r.Context(), id)
	if errors.Is(err, db.ErrReportNotFound) {
		w.WriteHeader(http.StatusAccepted)
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "generating"})
		return
	}
	if err != nil {
		s.log.Error("report get", "session", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "ошибка получения отчёта")
		return
	}
	// json.RawMessage — встроить JSON-массивы как есть (а не base64-строку).
	writeJSON(w, http.StatusOK, map[string]any{
		"overall":              overall,
		"grade_recommendation": gradeRec,
		"criteria":             json.RawMessage(criteria),
		"strengths":            json.RawMessage(strengths),
		"weaknesses":           json.RawMessage(weaknesses),
		"recommendations":      json.RawMessage(recommendations),
	})
}

// startReportGeneration — fire-and-forget генерация отчёта (WP-11):
// LLM-оценщик (fallback — детерминированный), сохранение, report_ready по WS.
func (s *Server) startReportGeneration(id int64) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*llm.DefaultTimeout)
		defer cancel()
		data, err := s.interviewer.GenerateReport(ctx, id)
		if err != nil {
			s.log.Warn("report: генерация не удалась", "session", id, "err", err)
			return
		}
		critJSON, _ := json.Marshal(data.Criteria)
		strJSON, _ := json.Marshal(data.Strengths)
		weakJSON, _ := json.Marshal(data.Weaknesses)
		recJSON, _ := json.Marshal(data.Recommendations)
		if err := s.reports.Save(ctx, id, data.Overall, data.GradeRecommendation,
			critJSON, strJSON, weakJSON, recJSON); err != nil {
			s.log.Error("report save", "session", id, "err", err)
			return
		}
		s.log.Info("отчёт готов", "session", id, "overall", data.Overall)
		s.engine.SendTo(id, map[string]any{"type": "report_ready", "overall": data.Overall})
	}()
}
