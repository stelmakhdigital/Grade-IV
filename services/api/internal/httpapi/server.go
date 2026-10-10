// Package httpapi — HTTP-хендлеры grade-api (ARCHITECTURE.md §4.1).
// WP-2: /healthz, /api/v1/auth/register, /api/v1/auth/login, /api/v1/auth/me.
package httpapi

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/config"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/interviewer"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/llm"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/metrics"
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
	reports     *db.ReportStore
	templates   *db.TemplateStore // итерация A: настраиваемые планы
	profiles    *db.ProfileStore  // итерация B: профили интервьюера
	engine      *session.Engine
	interviewer *interviewer.Interviewer
	voice       *voicesvc.Client
	log         *slog.Logger

	// wsSessions — живые WS-соединения по ID сессии (1 сессия — 1 соединение):
	// pause во время активного хода останавливает TTS-стрим (FR-S7).
	wsMu       sync.Mutex
	wsSessions map[int64]*wsSession
}

// New собирает сервер (LLM-провайдер по конфигу: реальный клиент или мок, LLM_MOCK).
func New(cfg *config.Config, database *sql.DB, dialect db.Dialect, log *slog.Logger) *Server {
	var provider llm.Provider
	if cfg.LLMMock {
		provider = newMockProvider(cfg)
	} else {
		provider = llm.NewClient(cfg.LLMBaseURL, cfg.LLMModel, cfg.LLMAPIKey).
			WithEnableThinking(cfg.LLMEnableThinking)
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
	reports := db.NewReportStore(database, dialect)
	templates := db.NewTemplateStore(database, dialect)
	if err := templates.EnsureDefaults(context.Background(), interviewer.DefaultPrograms()); err != nil {
		log.Warn("сид дефолтных шаблонов не удался (используется gradeProgram)", "err", err)
	}
	profiles := db.NewProfileStore(database, dialect)
	// Пресеты профилей интервьюера (идемпотентно, итерация B).
	if err := profiles.EnsurePresets(context.Background(), db.PresetProfiles()); err != nil {
		log.Warn("profiles: пресеты не сидированы", "err", err)
	}
	engine := session.New(sessions, users, log,
		session.WithPauseTimeout(time.Duration(cfg.PauseTimeoutS)*time.Second),
		session.WithSessionLimit(cfg.SessionLimitS),
		session.WithTemplates(templates))
	interviewer := interviewer.New(provider, sessions, log,
		interviewer.WithProfiles(profiles),
		interviewer.WithReports(reports))
	voice := voicesvc.NewClient(cfg.VoiceURL)
	return &Server{
		cfg:         cfg,
		users:       users,
		sessions:    sessions,
		submissions: submissions,
		whiteboards: whiteboards,
		reports:     reports,
		templates:   templates,
		profiles:    profiles,
		engine:      engine,
		interviewer: interviewer,
		voice:       voice,
		log:         log,
		wsSessions:  make(map[int64]*wsSession),
	}
}

// Engine — движок сессий (main: Stop при завершении).
func (s *Server) Engine() *session.Engine { return s.engine }

// newMockProvider — LLM-мок (LLM_MOCK=1): эхо по умолчанию; детерминированный
// замер (T-20261009001516) — LLM_MOCK_RESPONSE (фиксированный ответ) +
// LLM_MOCK_TOKENS_PER_S (скорость стриминга токенов, эмуляция реального узла).
func newMockProvider(cfg *config.Config) llm.Provider {
	m := llm.NewMockProvider()
	if cfg.LLMMockResponse != "" {
		resp := cfg.LLMMockResponse
		m.SetResponder(func(req llm.Request) (string, error) {
			// Приветствие (system-промпт «поприветствуй») — короткий ответ:
			// ускоряет замер (дренирование greeting), на измеряемый ход не влияет.
			for _, msg := range req.Messages {
				if msg.Role == llm.RoleSystem && strings.Contains(msg.Content, "поприветствуй") {
					return "Привет. Давайте начнём.", nil
				}
			}
			return resp, nil
		})
	}
	if cfg.LLMMockTokensPerS > 0 {
		m.SetTokenDelay(time.Duration(float64(time.Second) / cfg.LLMMockTokensPerS))
	}
	return m
}

// Handler — корневой обработчик с маршрутами.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	// Dev-эндпоинты диагностики — только при явном ENABLE_DEBUG=1 (в prod — 404,
	// без аутентификации наружу не выставляем).
	if s.cfg.DebugEndpoints {
		mux.HandleFunc("POST /debug/mic-report", s.handleDebugMicReport)
		mux.HandleFunc("OPTIONS /debug/mic-report", s.handleDebugMicReport)
	}
	mux.HandleFunc("GET /metrics", metrics.Handler)
	mux.HandleFunc("POST /api/v1/auth/register", s.handleRegister)
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	mux.Handle("GET /api/v1/auth/me", s.requireAuth(http.HandlerFunc(s.handleMe)))

	// Шаблоны интервью (итерация A).
	mux.Handle("GET /api/v1/templates", s.requireAuth(http.HandlerFunc(s.handleTemplatesList)))

	// Сессии (WP-3, ARCHITECTURE.md §4.1).
	mux.Handle("POST /api/v1/sessions", s.requireAuth(http.HandlerFunc(s.handleSessionsCreate)))
	mux.Handle("GET /api/v1/profiles", s.requireAuth(http.HandlerFunc(s.handleProfilesList)))
	mux.Handle("GET /api/v1/profiles/{id}", s.requireAuth(http.HandlerFunc(s.handleProfileGet)))
	mux.Handle("GET /api/v1/sessions", s.requireAuth(http.HandlerFunc(s.handleSessionsList)))
	mux.Handle("GET /api/v1/sessions/{id}", s.requireAuth(http.HandlerFunc(s.handleSessionGet)))
	mux.Handle("POST /api/v1/sessions/{id}/{action}", s.requireAuth(http.HandlerFunc(s.handleSessionAction)))
	// Более специфичный путь (литеральный сегмент) — приоритет над {action}.
	mux.Handle("POST /api/v1/sessions/{id}/runs", s.requireAuth(http.HandlerFunc(s.handleSessionRuns)))
	mux.Handle("GET /api/v1/sessions/{id}/events", s.requireAuth(http.HandlerFunc(s.handleSessionEvents)))
	mux.Handle("PUT /api/v1/sessions/{id}/whiteboard", s.requireAuth(http.HandlerFunc(s.handleWhiteboardPut)))
	mux.Handle("GET /api/v1/sessions/{id}/report", s.requireAuth(http.HandlerFunc(s.handleReportGet)))

	// Голосовой канал (WP-3, ADR-001): auth — токен в query/заголовке.
	mux.HandleFunc("GET /ws/session/{id}", s.handleSessionWS)
	return s.withLogging(mux)
}

// Dev-эндпоинт (audio-debug.html): принимает отчёт диагностики микрофона
// из браузера пользователя и сохраняет в FOR_RUN/debug/ для анализа.
func (s *Server) handleDebugMicReport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var payload map[string]any
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "некорректный JSON: "+err.Error())
		return
	}
	ts := time.Now().Format("20060102-150405")
	const dir = "FOR_RUN/debug"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "io_error", err.Error())
		return
	}
	b, _ := json.MarshalIndent(payload, "", "  ")
	if err := os.WriteFile(dir+"/mic-report-"+ts+".json", b, 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, "io_error", err.Error())
		return
	}
	s.log.Info("debug: mic-report сохранён", "file", dir+"/mic-report-"+ts+".json")
	w.WriteHeader(http.StatusNoContent)
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
	if r.status == 0 {
		r.status = code
	}
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
