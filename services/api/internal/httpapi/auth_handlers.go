package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/auth"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

type registerReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type userOut struct {
	ID        int64  `json:"id"`
	Email     string `json:"email"`
	CreatedAt string `json:"created_at"`
}

type tokenResp struct {
	Token             string  `json:"token"`
	ExpiresInS        int64   `json:"expires_in_s"`
	User              userOut `json:"user"`
	MinutesRemainingS int64   `json:"minutes_remaining_s"`
}

func toUserOut(u models.User) userOut {
	return userOut{ID: u.ID, Email: u.Email, CreatedAt: u.CreatedAt.UTC().Format(time.RFC3339)}
}

// completeAuth — выпуск токена и финальный ответ аутентификации.
func (s *Server) completeAuth(w http.ResponseWriter, r *http.Request, u models.User, status int) {
	token, err := auth.IssueToken(s.cfg.JWTSecret, u.ID, u.Email,
		time.Duration(s.cfg.JWTExpiryHours)*time.Hour)
	if err != nil {
		s.log.Error("issue token", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "ошибка выпуска токена")
		return
	}
	minutes, err := s.users.MinutesRemaining(r.Context(), u.ID)
	if err != nil {
		s.log.Error("minutes remaining", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "ошибка чтения баланса минут")
		return
	}
	writeJSON(w, status, tokenResp{
		Token:             token,
		ExpiresInS:        int64(s.cfg.JWTExpiryHours) * 3600,
		User:              toUserOut(u),
		MinutesRemainingS: minutes,
	})
}

// handleRegister — POST /api/v1/auth/register.
// Создаёт пользователя, выдаёт стартовый грант минут (FR-B1) и возвращает JWT.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "некорректное JSON-тело")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if !validEmail(email) {
		writeError(w, http.StatusBadRequest, "bad_email", "некорректный формат email")
		return
	}
	if len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "weak_password", "пароль должен быть не короче 8 символов")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		s.log.Error("hash password", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "ошибка хеширования пароля")
		return
	}

	u, err := s.users.CreateUser(r.Context(), email, hash)
	if err != nil {
		var exists *db.ErrEmailExists
		if errors.As(err, &exists) {
			writeError(w, http.StatusConflict, "email_exists", "email уже зарегистрирован")
			return
		}
		s.log.Error("create user", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "ошибка создания пользователя")
		return
	}

	if err := s.users.GrantMinutes(r.Context(), u.ID, nil, s.cfg.MinutesFreeS, "grant_free"); err != nil {
		s.log.Error("grant free minutes", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "ошибка выдачи бесплатных минут")
		return
	}

	s.completeAuth(w, r, u, http.StatusCreated)
}

// handleLogin — POST /api/v1/auth/login.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req registerReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "некорректное JSON-тело")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))

	u, err := s.users.GetUserByEmail(r.Context(), email)
	if err != nil && !errors.Is(err, db.ErrUserNotFound) {
		s.log.Error("get user", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "ошибка чтения пользователя")
		return
	}
	if errors.Is(err, db.ErrUserNotFound) || !auth.CheckPassword(u.PasswordHash, req.Password) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "неверный email или пароль")
		return
	}

	s.completeAuth(w, r, u, http.StatusOK)
}

// handleMe — GET /api/v1/auth/me (только с валидным JWT).
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u, err := s.users.GetUserByID(r.Context(), userIDFromContext(r.Context()))
	if errors.Is(err, db.ErrUserNotFound) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "пользователь не найден")
		return
	}
	if err != nil {
		s.log.Error("get user", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "ошибка чтения пользователя")
		return
	}
	minutes, err := s.users.MinutesRemaining(r.Context(), u.ID)
	if err != nil {
		s.log.Error("minutes remaining", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "ошибка чтения баланса минут")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		User              userOut `json:"user"`
		MinutesRemainingS int64   `json:"minutes_remaining_s"`
	}{toUserOut(u), minutes})
}

func validEmail(s string) bool {
	if len(s) == 0 || len(s) > 320 {
		return false
	}
	at := strings.Index(s, "@")
	if at <= 0 || at == len(s)-1 {
		return false
	}
	return strings.Contains(s[at+1:], ".")
}
