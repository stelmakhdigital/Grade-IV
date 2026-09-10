// Package httpapi — HTTP-хендлеры grade-api (ARCHITECTURE.md §4.1).
// WP-2: /healthz, /api/v1/auth/register, /api/v1/auth/login, /api/v1/auth/me.
package httpapi

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/config"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
)

// Server — HTTP-сервер: хендлеры + зависимости.
type Server struct {
	cfg   *config.Config
	users *db.UserStore
	log   *slog.Logger
}

// New собирает сервер.
func New(cfg *config.Config, database *sql.DB, dialect db.Dialect, log *slog.Logger) *Server {
	return &Server{cfg: cfg, users: db.NewUserStore(database, dialect), log: log}
}

// Handler — корневой обработчик с маршрутами.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /api/v1/auth/register", s.handleRegister)
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	mux.Handle("GET /api/v1/auth/me", s.requireAuth(http.HandlerFunc(s.handleMe)))
	return s.withLogging(mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "grade-api"})
}

// apiError — стандартный формат ошибки API.
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, apiError{Code: code, Message: msg})
}

// statusRecorder — захват статуса ответа для логов.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// withLogging — middleware: JSON-лог каждого запроса (NFR-9).
func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.log.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"dur_ms", time.Since(start).Milliseconds(),
		)
	})
}
