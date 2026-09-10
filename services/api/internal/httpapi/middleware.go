package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/auth"
)

type ctxKey int

const userIDKey ctxKey = 1

// requireAuth — middleware аутентификации по Bearer JWT.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, prefix) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "нет заголовка Authorization: Bearer <token>")
			return
		}
		claims, err := auth.ParseToken(s.cfg.JWTSecret, strings.TrimPrefix(header, prefix))
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "недействительный или просроченный токен")
			return
		}
		ctx := context.WithValue(r.Context(), userIDKey, claims.UserID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// userIDFromContext — ID пользователя из контекста (установлен requireAuth).
func userIDFromContext(ctx context.Context) int64 {
	if v, ok := ctx.Value(userIDKey).(int64); ok {
		return v
	}
	return 0
}
