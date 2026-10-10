// Package db — шаблоны интервью (настраиваемые планы под грейды).
//
// Template — план голосового интервью: набор блоков (title, focus, question_count),
// длительность, грейд/стек. При создании сессии выбирается шаблон (явно по id
// или дефолтный для грейда); ИИ ведёт голосовую стадию по блокам шаблона.
//
// Дефолты (4 шт. — по грейдам) сидируются при Migrate из gradeProgram
// (interviewer) — поведение до появления шаблонов сохраняется.
package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// TemplateBlock — блок программы интервью (один раздел: N вопросов + фокус).
type TemplateBlock struct {
	Title         string `json:"title"`
	Focus         string `json:"focus"`
	QuestionCount int    `json:"question_count"`
}

// Template — настраиваемый шаблон интервью.
type Template struct {
	ID         int64           `json:"id"`
	Name       string          `json:"name"`
	Grade      models.Grade    `json:"grade"`
	Stack      string          `json:"stack"`
	DurationS  int             `json:"duration_s"`
	Blocks     []TemplateBlock `json:"blocks"`
	IsDefault  bool            `json:"is_default"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

// ProgramString — программа для system-промпта (как gradeProgram, но из шаблона).
func (t *Template) ProgramString() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Программа голосового интервью (%s, шаблон «%s»): ", t.Grade, t.Name)
	for i, blk := range t.Blocks {
		n := blk.QuestionCount
		if n <= 0 {
			n = 1
		}
		fmt.Fprintf(&b, "(%d) %s (%d вопрос(а)): %s; ", i+1, blk.Title, n, blk.Focus)
	}
	b.WriteString("Вопросы по блокам по порядку, следуй ответам кандидата.")
	return b.String()
}

// TemplateStore — операции с шаблонами.
type TemplateStore struct {
	db *sql.DB
	d  Dialect
}

// NewTemplateStore — конструктор.
func NewTemplateStore(dbx *sql.DB, d Dialect) *TemplateStore {
	return &TemplateStore{db: dbx, d: d}
}

const templateCols = `id, name, grade, stack, duration_s, blocks, is_default, created_at, updated_at`

func (s *TemplateStore) scan(row interface{ Scan(...any) error }) (Template, error) {
	var t Template
	var blocksJSON string
	var isDefault int
	var created, updated string
	err := row.Scan(&t.ID, &t.Name, &t.Grade, &t.Stack, &t.DurationS,
		&blocksJSON, &isDefault, &created, &updated)
	if err != nil {
		return t, err
	}
	t.IsDefault = isDefault != 0
	t.CreatedAt, _ = time.Parse(time.RFC3339, created)
	t.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	if err := json.Unmarshal([]byte(blocksJSON), &t.Blocks); err != nil {
		return t, fmt.Errorf("блоки шаблона: %w", err)
	}
	return t, nil
}

// Get — шаблон по id.
func (s *TemplateStore) Get(ctx context.Context, id int64) (*Template, error) {
	row := s.db.QueryRowContext(ctx, s.d.q(
		"SELECT "+templateCols+" FROM interview_templates WHERE id = ?"), id)
	t, err := s.scan(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// List — шаблоны для (грейд, стек); стек "*" — универсальный (все стеки).
// Порядок: дефолтные первыми, затем по id.
func (s *TemplateStore) List(ctx context.Context, grade models.Grade, stack string) ([]*Template, error) {
	rows, err := s.db.QueryContext(ctx, s.d.q(
		"SELECT "+templateCols+" FROM interview_templates "+
			"WHERE grade = ? AND (stack = ? OR stack = '*') "+
			"ORDER BY is_default DESC, id ASC"), grade, stack)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Template
	for rows.Next() {
		t, err := s.scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}

// Default — дефолтный шаблон для (грейд, стек); если дефолтов несколько — первый.
func (s *TemplateStore) Default(ctx context.Context, grade models.Grade, stack string) (*Template, error) {
	row := s.db.QueryRowContext(ctx, s.d.q(
		"SELECT "+templateCols+" FROM interview_templates "+
			"WHERE grade = ? AND (stack = ? OR stack = '*') AND is_default = 1 "+
			"ORDER BY id ASC LIMIT 1"), grade, stack)
	t, err := s.scan(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// EnsureDefaults — сидирует дефолтные шаблоны (по одному на грейд) при пустой
// таблице. Дефолты передаёт вызывающий (из interviewer.DefaultPrograms) —
// db не зависит от interviewer. Идемпотентно: если для грейда есть хотя бы
// один шаблон — не трогаем.
func (s *TemplateStore) EnsureDefaults(ctx context.Context, defs []Template) error {
	for _, def := range defs {
		var n int
		err := s.db.QueryRowContext(ctx, s.d.q(
			"SELECT COUNT(*) FROM interview_templates WHERE grade = ?"), def.Grade).Scan(&n)
		if err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		blocksJSON, err := json.Marshal(def.Blocks)
		if err != nil {
			return err
		}
		now := time.Now().UTC().Format(time.RFC3339)
		isDefault := 0
		if def.IsDefault {
			isDefault = 1
		}
		_, err = s.db.ExecContext(ctx, s.d.q(
			"INSERT INTO interview_templates (name, grade, stack, duration_s, blocks, is_default, created_at, updated_at) "+
				"VALUES (?, ?, ?, ?, ?, ?, ?, ?)"),
			def.Name, def.Grade, def.Stack, def.DurationS, string(blocksJSON), isDefault, now, now)
		if err != nil {
			return err
		}
	}
	return nil
}

// Create — новый пользовательский шаблон (is_default=false).
func (s *TemplateStore) Create(ctx context.Context, t *Template) error {
	blocksJSON, err := json.Marshal(t.Blocks)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	isDefault := 0
	if t.IsDefault {
		isDefault = 1
	}
	res, err := s.db.ExecContext(ctx, s.d.q(
		"INSERT INTO interview_templates (name, grade, stack, duration_s, blocks, is_default, created_at, updated_at) "+
			"VALUES (?, ?, ?, ?, ?, ?, ?, ?)"),
		t.Name, t.Grade, t.Stack, t.DurationS, string(blocksJSON), isDefault, now, now)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	t.ID = id
	t.CreatedAt, t.UpdatedAt = time.Now().UTC(), time.Now().UTC()
	return nil
}
