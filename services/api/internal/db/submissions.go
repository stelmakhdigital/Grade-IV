package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// SubmissionStore — сдачи кода на стадии Live-Code (WP-6).
type SubmissionStore struct {
	db *sql.DB
	d  Dialect
}

// NewSubmissionStore создаёт хранилище.
func NewSubmissionStore(dbx *sql.DB, d Dialect) *SubmissionStore {
	return &SubmissionStore{db: dbx, d: d}
}

// Save сохраняет сдачу с результатом запуска.
func (s *SubmissionStore) Save(ctx context.Context, m models.Submission) (models.Submission, error) {
	res, err := s.db.ExecContext(ctx, s.d.q(`
		INSERT INTO submissions (session_id, task_id, files, action, exit_code, stdout, stderr,
			duration_ms, tests, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		m.SessionID, m.TaskID, m.Files, m.Action, m.ExitCode, m.Stdout, m.Stderr,
		m.DurationMS, m.Tests, nowRFC3339())
	if err != nil {
		return models.Submission{}, fmt.Errorf("save submission: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.Submission{}, fmt.Errorf("last insert id: %w", err)
	}
	var createdAt string
	err = s.db.QueryRowContext(ctx,
		s.d.q(`SELECT created_at FROM submissions WHERE id = ?`), id).Scan(&createdAt)
	if err != nil {
		return models.Submission{}, fmt.Errorf("read created_at: %w", err)
	}
	m.ID = id
	m.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return models.Submission{}, fmt.Errorf("parse created_at: %w", err)
	}
	return m, nil
}

// ListBySession — сдачи сессии по времени (отчёт WP-11).
func (s *SubmissionStore) ListBySession(ctx context.Context, sessionID int64) ([]models.Submission, error) {
	rows, err := s.db.QueryContext(ctx, s.d.q(`
		SELECT id, session_id, task_id, files, action, exit_code, stdout, stderr,
			duration_ms, tests, created_at
		FROM submissions WHERE session_id = ? ORDER BY id`), sessionID)
	if err != nil {
		return nil, fmt.Errorf("list submissions: %w", err)
	}
	defer rows.Close()
	out := make([]models.Submission, 0, 4)
	for rows.Next() {
		var m models.Submission
		var createdAt string
		if err := rows.Scan(&m.ID, &m.SessionID, &m.TaskID, &m.Files, &m.Action,
			&m.ExitCode, &m.Stdout, &m.Stderr, &m.DurationMS, &m.Tests, &createdAt); err != nil {
			return nil, fmt.Errorf("scan submission: %w", err)
		}
		m.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse created_at: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ErrNoSubmissions — у сессии нет сдач.
var ErrNoSubmissions = errors.New("сдач нет")
