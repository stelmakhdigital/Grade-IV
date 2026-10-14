package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// ErrSessionNotFound — сессия не найдена.
var ErrSessionNotFound = errors.New("сессия не найдена")

// SessionStore — операции с сессиями, событиями и лимитами (WP-3).
// Тайминги: активное время хранится в active_seconds (накопленное, обновляется
// движком на переходах/тиках); точные «точки» — started_at/paused_at/finished_at.
type SessionStore struct {
	db *sql.DB
	d  Dialect
}

// NewSessionStore создаёт хранилище.
func NewSessionStore(dbx *sql.DB, d Dialect) *SessionStore {
	return &SessionStore{db: dbx, d: d}
}

func parseTS(s string) (time.Time, error) { return time.Parse(time.RFC3339, s) }

// Create вставляет новую сессию (stage/status/длительность заданы вызывающим).
// TemplateID=0 → NULL (дефолтный шаблон грейда, итерация A). Program — программа
// шаблона (записывается на создание — сессия не меняется при правке шаблона).
func (s *SessionStore) Create(ctx context.Context, m models.Session) (models.Session, error) {
	var templateID any
	if m.TemplateID != 0 {
		templateID = m.TemplateID
	}
	var profileID any
	if m.ProfileID != 0 {
		profileID = m.ProfileID
	}
	res, err := s.db.ExecContext(ctx, s.d.q(`
		INSERT INTO sessions (user_id, grade, stack, stage, status, duration_limit_s,
			active_seconds, started_at, template_id, program, profile_id, tts_speaker)
		VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?)`),
		m.UserID, m.Grade, m.Stack, m.Stage, m.Status, m.DurationLimitS,
		m.StartedAt.UTC().Format(time.RFC3339), templateID, m.Program, profileID, m.TTSSpeaker)
	if err != nil {
		return models.Session{}, fmt.Errorf("create session: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.Session{}, fmt.Errorf("last insert id: %w", err)
	}
	return s.Get(ctx, id)
}

// Get — сессия по ID.
func (s *SessionStore) Get(ctx context.Context, id int64) (models.Session, error) {
	return s.scanSession(s.db.QueryRowContext(ctx,
		s.d.q(`SELECT id, user_id, grade, stack, stage, status, duration_limit_s,
			active_seconds, paused_at, started_at, finished_at, template_id, program, profile_id, tts_speaker
			FROM sessions WHERE id = ?`), id))
}

// GetOwned — сессия, если она принадлежит пользователю; иначе ErrSessionNotFound.
func (s *SessionStore) GetOwned(ctx context.Context, id, userID int64) (models.Session, error) {
	m, err := s.scanSession(s.db.QueryRowContext(ctx,
		s.d.q(`SELECT id, user_id, grade, stack, stage, status, duration_limit_s,
			active_seconds, paused_at, started_at, finished_at, template_id, program, profile_id, tts_speaker
			FROM sessions WHERE id = ? AND user_id = ?`), id, userID))
	if errors.Is(err, ErrSessionNotFound) {
		return m, err
	}
	return m, err
}

// ListByUser — сессии пользователя (новые первыми).
func (s *SessionStore) ListByUser(ctx context.Context, userID int64) ([]models.Session, error) {
	rows, err := s.db.QueryContext(ctx,
		s.d.q(`SELECT id, user_id, grade, stack, stage, status, duration_limit_s,
			active_seconds, paused_at, started_at, finished_at, template_id, program, profile_id, tts_speaker
			FROM sessions WHERE user_id = ? ORDER BY id DESC`), userID)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	out := make([]models.Session, 0, 8)
	for rows.Next() {
		m, err := s.scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetStage — смена стадии.
func (s *SessionStore) SetStage(ctx context.Context, id int64, stage models.Stage) error {
	if _, err := s.db.ExecContext(ctx,
		s.d.q(`UPDATE sessions SET stage = ? WHERE id = ?`), stage, id); err != nil {
		return fmt.Errorf("set stage: %w", err)
	}
	return nil
}

// MarkPaused — пауза: статус paused, paused_at, накопленные активные секунды.
func (s *SessionStore) MarkPaused(ctx context.Context, id int64, activeSeconds float64, at time.Time) error {
	if _, err := s.db.ExecContext(ctx, s.d.q(`
		UPDATE sessions SET status = ?, paused_at = ?, active_seconds = ? WHERE id = ?`),
		models.StatusPaused, at.UTC().Format(time.RFC3339), activeSeconds, id); err != nil {
		return fmt.Errorf("mark paused: %w", err)
	}
	return nil
}

// MarkActive — снятие с паузы (paused_at очищается).
func (s *SessionStore) MarkActive(ctx context.Context, id int64) error {
	if _, err := s.db.ExecContext(ctx,
		s.d.q(`UPDATE sessions SET status = ?, paused_at = NULL WHERE id = ?`),
		models.StatusActive, id); err != nil {
		return fmt.Errorf("mark active: %w", err)
	}
	return nil
}

// PersistActiveSeconds — периодическое сохранение накопленного времени (до 5 с потери).
func (s *SessionStore) PersistActiveSeconds(ctx context.Context, id int64, activeSeconds float64) error {
	if _, err := s.db.ExecContext(ctx,
		s.d.q(`UPDATE sessions SET active_seconds = ? WHERE id = ?`), activeSeconds, id); err != nil {
		return fmt.Errorf("persist active seconds: %w", err)
	}
	return nil
}

// Finalize — финализация: статус (finished/aborted), активные секунды, finished_at.
func (s *SessionStore) Finalize(ctx context.Context, id int64, status models.Status, activeSeconds float64, at time.Time) error {
	if _, err := s.db.ExecContext(ctx, s.d.q(`
		UPDATE sessions SET status = ?, active_seconds = ?, finished_at = ?, paused_at = NULL
		WHERE id = ?`),
		status, activeSeconds, at.UTC().Format(time.RFC3339), id); err != nil {
		return fmt.Errorf("finalize: %w", err)
	}
	return nil
}

// ListByStatus — сессии с указанным статусом (рекавери движка при старте).
func (s *SessionStore) ListByStatus(ctx context.Context, status models.Status) ([]models.Session, error) {
	rows, err := s.db.QueryContext(ctx,
		s.d.q(`SELECT id, user_id, grade, stack, stage, status, duration_limit_s,
			active_seconds, paused_at, started_at, finished_at, template_id, program, profile_id, tts_speaker
			FROM sessions WHERE status = ?`), status)
	if err != nil {
		return nil, fmt.Errorf("list by status: %w", err)
	}
	defer rows.Close()
	out := make([]models.Session, 0, 4)
	for rows.Next() {
		m, err := s.scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AddEvent — событие сессии; seq = max(seq)+1 (уникальный индекc (session_id, seq)).
// spec-14/A3: SELECT+INSERT был неатомарен — при параллельных событиях одной
// сессии (WS-ходы движка + REST code_run/whiteboard_save) конфликт
// uq_events_session_seq навсегда терял событие. Ретраим по конфликту индекса.
func (s *SessionStore) AddEvent(ctx context.Context, sessionID int64, kind string, data []byte) (int, error) {
	for attempt := 0; attempt < 4; attempt++ {
		var seq int
		err := s.db.QueryRowContext(ctx,
			s.d.q(`SELECT COALESCE(MAX(seq), 0) + 1 FROM session_events WHERE session_id = ?`),
			sessionID).Scan(&seq)
		if err != nil {
			return 0, fmt.Errorf("event seq: %w", err)
		}
		if _, err := s.db.ExecContext(ctx, s.d.q(`
			INSERT INTO session_events (session_id, seq, ts, kind, data) VALUES (?, ?, ?, ?, ?)`),
			sessionID, seq, nowRFC3339(), kind, data); err == nil {
			return seq, nil
		} else if !strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return 0, fmt.Errorf("add event: %w", err)
		}
		// Конкурент вставил seq первым — пробуем заново (короткая пауза).
		select {
		case <-ctx.Done():
			return 0, fmt.Errorf("add event: %w", ctx.Err())
		case <-time.After(2 * time.Millisecond):
		}
	}
	return 0, fmt.Errorf("add event: исчерпаны ретраи seq")
}

// ListEvents — события сессии по возрастанию seq.
func (s *SessionStore) ListEvents(ctx context.Context, sessionID int64) ([]models.SessionEvent, error) {
	rows, err := s.db.QueryContext(ctx,
		s.d.q(`SELECT id, session_id, seq, ts, kind, data FROM session_events
			WHERE session_id = ? ORDER BY seq`), sessionID)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()
	out := make([]models.SessionEvent, 0, 16)
	for rows.Next() {
		var e models.SessionEvent
		var ts string
		if err := rows.Scan(&e.ID, &e.SessionID, &e.Seq, &ts, &e.Kind, &e.Data); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		if e.Data == nil {
			e.Data = []byte("{}")
		}
		e.TS, err = parseTS(ts)
		if err != nil {
			return nil, fmt.Errorf("parse event ts: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListEventsTail — последние N событий сессии по возрастанию seq (spec-14/A6:
// interviewer читал ВСЮ историю событий на каждый LLM-ход; для промпта
// достаточно хвоста — LIMIT на стороне SQL).
func (s *SessionStore) ListEventsTail(ctx context.Context, sessionID int64, n int) ([]models.SessionEvent, error) {
	rows, err := s.db.QueryContext(ctx,
		s.d.q(`SELECT id, session_id, seq, ts, kind, data FROM session_events
			WHERE session_id = ? ORDER BY seq DESC LIMIT ?`), sessionID, n)
	if err != nil {
		return nil, fmt.Errorf("list events tail: %w", err)
	}
	defer rows.Close()
	out := make([]models.SessionEvent, 0, 16)
	for rows.Next() {
		var e models.SessionEvent
		var ts string
		if err := rows.Scan(&e.ID, &e.SessionID, &e.Seq, &ts, &e.Kind, &e.Data); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		if e.Data == nil {
			e.Data = []byte("{}")
		}
		e.TS, err = parseTS(ts)
		if err != nil {
			return nil, fmt.Errorf("parse event ts: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// DESC → reverse для хронологии (без сортировки в SQL).
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// HasEventKind — EXISTS-проверка без чтения всех событий (spec-14/A6:
// hasAIUtterance сканировал всю историю на каждое WS-подключение).
func (s *SessionStore) HasEventKind(ctx context.Context, sessionID int64, kind string) (bool, error) {
	var ok bool
	err := s.db.QueryRowContext(ctx,
		s.d.q(`SELECT EXISTS(SELECT 1 FROM session_events WHERE session_id = ? AND kind = ?)`),
		sessionID, kind).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("has event kind: %w", err)
	}
	return ok, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func (s *SessionStore) scanSession(row rowScanner) (models.Session, error) {
	var m models.Session
	var grade, stack, stage, status string
	var startedAt string
	var pausedAt, finishedAt sql.NullString
	var templateID sql.NullInt64
	var program string
	var profileID sql.NullInt64
	var ttsSpeaker string
	err := row.Scan(&m.ID, &m.UserID, &grade, &stack, &stage, &status,
		&m.DurationLimitS, &m.ActiveSeconds, &pausedAt, &startedAt, &finishedAt, &templateID, &program, &profileID, &ttsSpeaker)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Session{}, ErrSessionNotFound
	}
	if err != nil {
		return models.Session{}, fmt.Errorf("scan session: %w", err)
	}
	m.Grade, m.Stack, m.Stage, m.Status = models.Grade(grade), models.Stack(stack), models.Stage(stage), models.Status(status)
	m.StartedAt, err = parseTS(startedAt)
	if err != nil {
		return models.Session{}, fmt.Errorf("parse started_at: %w", err)
	}
	if pausedAt.Valid && pausedAt.String != "" {
		t, err := parseTS(pausedAt.String)
		if err != nil {
			return models.Session{}, fmt.Errorf("parse paused_at: %w", err)
		}
		m.PausedAt = &t
	}
	if finishedAt.Valid && finishedAt.String != "" {
		t, err := parseTS(finishedAt.String)
		if err != nil {
			return models.Session{}, fmt.Errorf("parse finished_at: %w", err)
		}
		m.FinishedAt = &t
	}
	if templateID.Valid {
		m.TemplateID = templateID.Int64
	}
	m.Program = program
	if profileID.Valid {
		m.ProfileID = profileID.Int64
	}
	m.TTSSpeaker = ttsSpeaker
	return m, nil
}
