package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/session"
)

// REST-хендлеры CRUD сессий: создание, список, детальная, действия-хелперы

// DTO сессии для REST (ARCHITECTURE.md §4.1).
type sessionDTO struct {
	ID    int64  `json:"id"`
	Grade string `json:"grade"`
	Stack string `json:"stack"`
	// spec-20: трек роли в DTO списка/детальной — кабинет показывает роль
	// вместо сырого грейда, отчёт завершённой сессии берёт мету трека
	// (пусто = легаси-сессия на backend).
	TrackID string `json:"track_id,omitempty"`
	// BaseGrade — грейд по резюме (ИИ), если интервью подбиралось по нему:
	// Grade при этом — цель на шаг выше. Пусто — старт с явным грейдом.
	BaseGrade      string     `json:"base_grade,omitempty"`
	Stage          string     `json:"stage"`
	Status         string     `json:"status"`
	DurationLimitS int        `json:"duration_limit_s"`
	ActiveSeconds  float64    `json:"active_seconds"`
	TimeLeftS      int        `json:"time_left_s"`
	StartedAt      time.Time  `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	PausedAt       *time.Time `json:"paused_at,omitempty"`
	ProfileID      int64      `json:"profile_id"`
	TTSSpeaker     string     `json:"tts_speaker"` // F1: выбранный голос интервьюера
	// ResumeTitle/ResumeStacks — должность и языки по резюме. Заголовок
	// интервью и кодовая стадия обязаны им соответствовать.
	ResumeTitle  string         `json:"resume_title,omitempty"`
	ResumeStacks []models.Stack `json:"resume_stacks,omitempty"`
}

func toDTO(m models.Session, snap *session.Snapshot) sessionDTO {
	d := sessionDTO{
		ID:             m.ID,
		Grade:          string(m.Grade),
		Stack:          string(m.Stack),
		TrackID:        m.TrackID,
		BaseGrade:      string(m.BaseGrade),
		ResumeTitle:    m.ResumeTitle,
		ResumeStacks:   m.ResumeStacks,
		Stage:          string(m.Stage),
		Status:         string(m.Status),
		DurationLimitS: m.DurationLimitS,
		ActiveSeconds:  m.ActiveSeconds,
		TimeLeftS:      0,
		StartedAt:      m.StartedAt,
		FinishedAt:     m.FinishedAt,
		PausedAt:       m.PausedAt,
		ProfileID:      m.ProfileID,
		TTSSpeaker:     m.TTSSpeaker,
	}
	if snap != nil {
		d.Stage = string(snap.Stage)
		d.Status = string(snap.Status)
		d.ActiveSeconds = snap.ActiveSeconds
		d.TimeLeftS = snap.TimeLeftS
	}
	return d
}

// handleSessionsCreate — POST /api/v1/sessions {grade?, stack?, profile_id?, track_id?}.
// 201: {id, ws_url, duration_limit_s}; 404 — profile_id не найден (Итерация B).
// Пустой grade — роль и грейд берёт ИИ из резюме кандидата (кабинет их не спрашивает).
func (s *Server) handleSessionsCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Grade      models.Grade `json:"grade"`
		Stack      models.Stack `json:"stack"`
		TrackID    string       `json:"track_id,omitempty"`    // spec-18: трек роли; пусто = backend
		TemplateID int64        `json:"template_id,omitempty"` // итерация A: 0 = дефолтный шаблон грейда
		ProfileID  int64        `json:"profile_id,omitempty"`  // итерация B: 0 = balanced/standard
		TTSSpeaker string       `json:"tts_speaker,omitempty"` // F1: голос интервьюера; пусто = дефолт
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "ожидается JSON {grade, stack}")
		return
	}
	uid := userIDFromContext(r.Context())
	grade := body.Grade
	var opts []session.CreateOption
	if body.TrackID != "" {
		opts = append(opts, session.WithTrack(strings.TrimSpace(body.TrackID)))
	}
	if grade == "" {
		profile := s.inferRole(r.Context(), uid)
		t, err := s.tracks.GetOrDefault(profile.TrackID)
		if err != nil || t == nil {
			t, _ = s.tracks.GetOrDefault(models.DefaultTrackID)
		}
		if t == nil {
			writeError(w, http.StatusInternalServerError, "internal", "роли собеседования не загружены")
			return
		}
		opts = append(opts, session.WithTrack(t.ID), session.WithBaseGrade(profile.Base),
			session.WithResumeProfile(profile.Title, profile.Stacks))
		grade = targetGrade(profile.Base, t.Grades)
		// Стек выбирает движок: внутри стеков роли — по резюме, иначе рантайм роли.
		body.Stack = t.Runtime()
	}
	if body.TemplateID > 0 {
		opts = append(opts, session.WithTemplate(body.TemplateID))
	}
	if body.ProfileID > 0 {
		p, err := s.profiles.Get(r.Context(), body.ProfileID)
		if err == nil {
			opts = append(opts, session.WithProfile(p.ID))
		} else if errors.Is(err, db.ErrProfileNotFound) {
			writeError(w, http.StatusNotFound, "profile_not_found",
				"профиль интервьюера не найден: "+strconv.FormatInt(body.ProfileID, 10))
			return
		} else {
			s.log.Error("get profile", "err", err)
			writeError(w, http.StatusInternalServerError, "internal", "не удалось получить профиль")
			return
		}
	}
	if body.TTSSpeaker != "" {
		opts = append(opts, session.WithTTSSpeaker(strings.TrimSpace(body.TTSSpeaker)))
	}
	m, err := s.engine.Create(r.Context(), uid, grade, body.Stack, opts...)
	switch {
	case err == nil:
		writeJSON(w, http.StatusCreated, map[string]any{
			"id": m.ID, "ws_url": "/ws/session/" + strconv.FormatInt(m.ID, 10),
			"duration_limit_s": m.DurationLimitS, "template_id": m.TemplateID,
			"track_id": m.TrackID,
			"stack":    m.Stack, // spec-22: рантайм практики роли (пусто — без практики)
			// Грейд по резюме (пусто — резюме не определялось): цель была на шаг выше.
			"base_grade": m.BaseGrade,
		})
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
		// spec-14/A10: снимок только для живых runtime (map-чек без похода в БД);
		// раньше список из N сессий давал N+1 запрос (engineSnapshot при
		// rt==nil делал store.Get на каждую строку).
		var snap *session.Snapshot
		if s.engine.HasRuntime(m.ID) {
			snap = s.engineSnapshot(m.ID)
		}
		out = append(out, toDTO(m, snap))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSessionGet — GET /api/v1/sessions/{id}.
func (s *Server) handleSessionGet(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSessionID(w, r)
	if !ok {
		return
	}
	m, ok := s.ownedSession(w, r, id)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, toDTO(m, s.engineSnapshot(id)))
}

// ownedSession — сессия с проверкой владения; при ошибке пишет ответ и возвращает ok=false.
func (s *Server) ownedSession(w http.ResponseWriter, r *http.Request, id int64) (models.Session, bool) {
	m, err := s.sessions.GetOwned(r.Context(), id, userIDFromContext(r.Context()))
	if errors.Is(err, db.ErrSessionNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "сессия не найдена")
		return models.Session{}, false
	}
	if err != nil {
		s.log.Error("get session", "session", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "не удалось получить сессию")
		return models.Session{}, false
	}
	return m, true
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

// clientError — единый маппинг ошибки на (код, сообщение) для клиента. Наружу
// идут только наши sentinel-тексты: transport-ошибки, пути и SQL в ответе —
// это утечка внутреннего устройства и непонятный кандидату текст. Подробное
// «почему» читается в логе (вызывающий обязан залоировать default-ветку).
func clientError(err error) (code, msg string) {
	switch {
	case errors.Is(err, db.ErrSessionNotFound):
		return "not_found", "сессия не найдена"
	case errors.Is(err, session.ErrSessionEnded):
		return "session_ended", err.Error()
	case errors.Is(err, session.ErrIllegalStage),
		errors.Is(err, session.ErrIllegalStatus),
		errors.Is(err, session.ErrNotAttached),
		errors.Is(err, session.ErrStageNotInScope):
		return "invalid_state", err.Error()
	default:
		return "internal", "внутренняя ошибка"
	}
}

// writeSessionEngineError — маппинг ошибок движка на коды HTTP.
func (s *Server) writeSessionEngineError(w http.ResponseWriter, err error) {
	code, msg := clientError(err)
	status := http.StatusInternalServerError
	switch code {
	case "not_found":
		status = http.StatusNotFound
	case "session_ended", "invalid_state":
		status = http.StatusConflict
	default:
		s.log.Error("session engine", "err", err)
	}
	writeError(w, status, code, msg)
}
