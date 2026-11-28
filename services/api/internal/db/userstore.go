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

// ErrUserNotFound — пользователь не найден.
var ErrUserNotFound = errors.New("пользователь не найден")

// ErrEmailExists — email уже зарегистрирован.
type ErrEmailExists struct{ Email string }

func (e *ErrEmailExists) Error() string {
	return fmt.Sprintf("email %s уже зарегистрирован", e.Email)
}

// UserStore — операции с пользователями и профилем резюме.
type UserStore struct {
	db *sql.DB
	d  Dialect
}

// NewUserStore создаёт хранилище.
func NewUserStore(dbx *sql.DB, d Dialect) *UserStore {
	return &UserStore{db: dbx, d: d}
}

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }

// CreateUser создаёт пользователя (email нормализуется к нижнему регистру).
// Если email уже зарегистрирован — возвращается *ErrEmailExists.
// Итерация 11: первый пользователь в БД (свежая БД) = admin (is_admin=true).
func (s *UserStore) CreateUser(ctx context.Context, email, passwordHash string) (models.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var n int
	if err := s.db.QueryRowContext(ctx, s.d.q(`SELECT COUNT(*) FROM users`)).Scan(&n); err != nil {
		return models.User{}, fmt.Errorf("count users: %w", err)
	}
	isAdmin := n == 0 // первый в свежей БД — admin
	// RETURNING id понимают обе СУБД (sqlite с 3.35), LastInsertId в lib/pq нет.
	var id int64
	err := s.db.QueryRowContext(ctx,
		s.d.q(`INSERT INTO users (email, password_hash, created_at, is_admin) VALUES (?, ?, ?, ?) RETURNING id`),
		email, passwordHash, nowRFC3339(), isAdmin).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return models.User{}, &ErrEmailExists{Email: email}
		}
		return models.User{}, fmt.Errorf("create user: %w", err)
	}
	return s.GetUserByID(ctx, id)
}

// GetUserByEmail — по email (в любом регистре).
func (s *UserStore) GetUserByEmail(ctx context.Context, email string) (models.User, error) {
	return s.scanUser(ctx, s.d.q(`SELECT id, email, password_hash, created_at, is_admin FROM users WHERE email = ?`),
		strings.ToLower(strings.TrimSpace(email)))
}

// GetUserByID — по идентификатору.
func (s *UserStore) GetUserByID(ctx context.Context, id int64) (models.User, error) {
	return s.scanUser(ctx, s.d.q(`SELECT id, email, password_hash, created_at, is_admin FROM users WHERE id = ?`), id)
}

// GetVacancy — текст вакансии кандидата (F6, итерация 12). Нет пользователя → "".
func (s *UserStore) GetVacancy(ctx context.Context, userID int64) (string, error) {
	var text string
	err := s.db.QueryRowContext(ctx,
		s.d.q(`SELECT vacancy_text FROM users WHERE id = ?`), userID).Scan(&text)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get vacancy: %w", err)
	}
	return text, nil
}

// SetVacancy — сохранить/сбросить текст вакансии кандидата (F6, итерация 12).
// Пустая строка = сброс.
func (s *UserStore) SetVacancy(ctx context.Context, userID int64, text string) error {
	_, err := s.db.ExecContext(ctx,
		s.d.q(`UPDATE users SET vacancy_text = ? WHERE id = ?`), text, userID)
	if err != nil {
		return fmt.Errorf("set vacancy: %w", err)
	}
	return nil
}

func (s *UserStore) scanUser(ctx context.Context, query string, arg any) (models.User, error) {
	var u models.User
	var createdAt string
	err := s.db.QueryRowContext(ctx, query, arg).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &createdAt, &u.IsAdmin)
	if errors.Is(err, sql.ErrNoRows) {
		return models.User{}, ErrUserNotFound
	}
	if err != nil {
		return models.User{}, fmt.Errorf("get user: %w", err)
	}
	u.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return models.User{}, fmt.Errorf("parse created_at: %w", err)
	}
	return u, nil
}

// Resume — профиль кандидата для ИИ-интервьюера: своё описание + текст, вытащенный
// из файла резюме.
type Resume struct {
	About string
	Text  string
	// FileName — исходное имя файла (показать кандидату).
	FileName string
	// Path — куда файл лёг на диске ('' — файла нет). Путь генерирует сервер,
	// имя пользователя в него не попадает.
	Path string
}

// Empty — истинно, если резюме нет вовсе (ни описания, ни файла).
func (r Resume) Empty() bool {
	return strings.TrimSpace(r.About) == "" && strings.TrimSpace(r.Text) == "" && r.Path == ""
}

// Prompt — текст для промпта интервьюера: описание кандидата + содержимое файла.
// Пустой профиль → "" (блок резюме в промпт не попадает).
func (r Resume) Prompt() string {
	var b strings.Builder
	if a := strings.TrimSpace(r.About); a != "" {
		b.WriteString(a)
	}
	if t := strings.TrimSpace(r.Text); t != "" {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(t)
	}
	return b.String()
}

// GetResume — профиль резюме кандидата. Нет пользователя — пустой Resume.
func (s *UserStore) GetResume(ctx context.Context, userID int64) (Resume, error) {
	var r Resume
	err := s.db.QueryRowContext(ctx,
		s.d.q(`SELECT resume_about, resume_text, resume_file, resume_path FROM users WHERE id = ?`), userID).
		Scan(&r.About, &r.Text, &r.FileName, &r.Path)
	if errors.Is(err, sql.ErrNoRows) {
		return Resume{}, nil
	}
	if err != nil {
		return Resume{}, fmt.Errorf("get resume: %w", err)
	}
	return r, nil
}

// SetResume — сохранить/сбросить профиль резюме (пустые поля = сброс).
func (s *UserStore) SetResume(ctx context.Context, userID int64, r Resume) error {
	_, err := s.db.ExecContext(ctx,
		s.d.q(`UPDATE users SET resume_about = ?, resume_text = ?, resume_file = ?, resume_path = ? WHERE id = ?`),
		r.About, r.Text, r.FileName, r.Path, userID)
	if err != nil {
		return fmt.Errorf("set resume: %w", err)
	}
	return nil
}

// ResumeGuessGet — подбор роли и грейда по резюме (JSON, см. httpapi).
// Пустая строка — подбор не считался или ИИ не ответил.
func (s *UserStore) ResumeGuessGet(ctx context.Context, userID int64) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx,
		s.d.q(`SELECT resume_guess FROM users WHERE id = ?`), userID).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get resume guess: %w", err)
	}
	return v, nil
}

// ResumeGuessSet — сохранить подбор (” — сбросить).
func (s *UserStore) ResumeGuessSet(ctx context.Context, userID int64, guess string) error {
	_, err := s.db.ExecContext(ctx,
		s.d.q(`UPDATE users SET resume_guess = ? WHERE id = ?`), guess, userID)
	if err != nil {
		return fmt.Errorf("set resume guess: %w", err)
	}
	return nil
}

func isUniqueViolation(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") || strings.Contains(msg, "duplicate key")
}
