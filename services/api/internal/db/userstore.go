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

// UserStore — операции с пользователями и проводки учёта минут.
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
func (s *UserStore) CreateUser(ctx context.Context, email, passwordHash string) (models.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	res, err := s.db.ExecContext(ctx,
		s.d.q(`INSERT INTO users (email, password_hash, created_at) VALUES (?, ?, ?)`),
		email, passwordHash, nowRFC3339())
	if err != nil {
		if isUniqueViolation(err) {
			return models.User{}, &ErrEmailExists{Email: email}
		}
		return models.User{}, fmt.Errorf("create user: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.User{}, fmt.Errorf("last insert id: %w", err)
	}
	return s.GetUserByID(ctx, id)
}

// GetUserByEmail — по email (в любом регистре).
func (s *UserStore) GetUserByEmail(ctx context.Context, email string) (models.User, error) {
	return s.scanUser(ctx, s.d.q(`SELECT id, email, password_hash, created_at FROM users WHERE email = ?`),
		strings.ToLower(strings.TrimSpace(email)))
}

// GetUserByID — по идентификатору.
func (s *UserStore) GetUserByID(ctx context.Context, id int64) (models.User, error) {
	return s.scanUser(ctx, s.d.q(`SELECT id, email, password_hash, created_at FROM users WHERE id = ?`), id)
}

func (s *UserStore) scanUser(ctx context.Context, query string, arg any) (models.User, error) {
	var u models.User
	var createdAt string
	err := s.db.QueryRowContext(ctx, query, arg).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &createdAt)
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

// GrantMinutes добавляет проводку в ledger: seconds > 0 — грант, < 0 — расход.
func (s *UserStore) GrantMinutes(ctx context.Context, userID int64, sessionID *int64, seconds int, reason string) error {
	_, err := s.db.ExecContext(ctx,
		s.d.q(`INSERT INTO minutes_ledger (user_id, session_id, delta_seconds, reason, created_at) VALUES (?, ?, ?, ?, ?)`),
		userID, sessionID, seconds, reason, nowRFC3339())
	if err != nil {
		return fmt.Errorf("grant minutes: %w", err)
	}
	return nil
}

// MinutesRemaining — остаток минут пользователя (SUM по ledger).
func (s *UserStore) MinutesRemaining(ctx context.Context, userID int64) (int64, error) {
	var sum int64
	err := s.db.QueryRowContext(ctx,
		s.d.q(`SELECT COALESCE(SUM(delta_seconds), 0) FROM minutes_ledger WHERE user_id = ?`),
		userID).Scan(&sum)
	if err != nil {
		return 0, fmt.Errorf("minutes remaining: %w", err)
	}
	return sum, nil
}

func isUniqueViolation(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") || strings.Contains(msg, "duplicate key")
}
