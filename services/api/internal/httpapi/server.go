// Package httpapi — HTTP-хендлеры grade-api (ARCHITECTURE.md §4.1).
// WP-2: /healthz, /api/v1/auth/register, /api/v1/auth/login, /api/v1/auth/me.
package httpapi

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/config"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/interviewer"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/llm"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/session"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/voicesvc"
)

// Server — HTTP-сервер: хендлеры + зависимости.
type Server struct {
	cfg         *config.Config
	users       *db.UserStore
	sessions    *db.SessionStore
	submissions *db.SubmissionStore
	whiteboards *db.WhiteboardStore
	engine      *session.Engine
	interviewer *interviewer.Interviewer
	voice       *voicesvc.Client
	log         *slog.Logger
}

// New собирает сервер (LLM-провайдер по конфигу: реальный клиент или мок, LLM_MOCK).
func New(cfg *config.Config, database *sql.DB, dialect db.Dialect, log *slog.Logger) *Server {
	var provider llm.Provider
	if cfg.LLMMock {
		provider = llm.NewMockProvider()
	} else {
		provider = llm.NewClient(cfg.LLMBaseURL, cfg.LLMModel, cfg.LLMAPIKey)
	}
	return NewWithLLM(cfg, database, dialect, log, provider)
}

// NewWithLLM — сборка с явным LLM-провайдером (тесты/ди, ADR-005).
func NewWithLLM(cfg *config.Config, database *sql.DB, dialect db.Dialect, log *slog.Logger,
	provider llm.Provider) *Server {
	users := db.NewUserStore(database, dialect)
	sessions := db.NewSessionStore(database, dialect)
	submissions := db.NewSubmissionStore(database, dialect)
	whiteboards := db.NewWhiteboardStore(database, dialect)
	engine := session.New(sessions, users, log,
		session.WithPauseTimeout(time.Duration(cfg.PauseTimeoutS)*time.Second))
	interviewer := interviewer.New(provider, sessions, log)
	voice := voicesvc.NewClient(cfg.VoiceURL)
	return &Server{
		cfg:         cfg,
		users:       users,
		sessions:    sessions,
		submissions: submissions,
		whiteboards: whiteboards,
		engine:      engine,
		interviewer: interviewer,
		voice:       voice,
		log:         log,
	}
}

// Engine — движок сессий (main: Stop при завершении).
func (s *Server) Engine() *session.Engine { return s.engine }

// Handler — корневой обработчик с маршрутами.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /api/v1/auth/register", s.handleRegister)
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	mux.Handle("GET /api/v1/auth/me", s.requireAuth(http.HandlerFunc(s.handleMe)))

	// Сессии (WP-3, ARCHITECTURE.md §4.1).
	mux.Handle("POST /api/v1/sessions", s.requireAuth(http.HandlerFunc(s.handleSessionsCreate)))
	mux.Handle("GET /api/v1/sessions", s.requireAuth(http.HandlerFunc(s.handleSessionsList)))
	mux.Handle("GET /api/v1/sessions/{id}", s.requireAuth(http.HandlerFunc(s.handleSessionGet)))
	mux.Handle("POST /api/v1/sessions/{id}/{action}", s.requireAuth(http.HandlerFunc(s.handleSessionAction)))
	// Более специфичный путь (литеральный сегмент) — приоритет над {action}.
	mux.Handle("POST /api/v1/sessions/{id}/runs", s.requireAuth(http.HandlerFunc(s.handleSessionRuns)))
	mux.Handle("GET /api/v1/sessions/{id}/events", s.requireAuth(http.HandlerFunc(s.handleSessionEvents)))
	mux.Handle("PUT /api/v1/sessions/{id}/whiteboard", s.requireAuth(http.HandlerFunc(s.handleWhiteboardPut)))

	// Голосовой канал (WP-3, ADR-001): auth — токен в query/заголовке.
	mux.HandleFunc("GET /ws/session/{id}", s.handleSessionWS)
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

// Hijack — прокидывание к внутреннему writer (нужно для WS-апгрейда, ADR-001):
// http.ResponseWriter не включает Hijack в методную сигнатуру, поэтому
// встраивание интерфейса его не промует.
func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("ResponseWriter не поддерживает http.Hijacker")
	}
	return hj.Hijack()
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
