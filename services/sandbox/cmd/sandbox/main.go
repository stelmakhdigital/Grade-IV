// Command sandbox — сандбокс выполнения кода кандидата (ARCHITECTURE.md §4.4, ADR-003).
// WP-6: runner (docker --network=none + лимиты / subprocess dev-fallback),
// банк задач (go:embed), POST /runs, GET /tasks, GET /healthz.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/sandbox/internal/runner"
	"github.com/stelmakhdigital/grade-iv/services/sandbox/internal/tasks"
)

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// buildMux — маршруты сервиса (вынесено для тестов).
func buildMux(mode string, bank *tasks.Bank, rn *runner.Runner, log *slog.Logger) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "ok",
			"service": "grade-sandbox",
			"mode":    mode,
			"tasks":   bank.Count(),
		})
	})
	mux.HandleFunc("GET /api/v1/tasks", handleTasksList(bank))
	mux.HandleFunc("POST /api/v1/sessions/{id}/runs", handleRun(mode, bank, rn, log))
	return mux
}

// handleTasksList — GET /api/v1/tasks?stack=go&grade=middle.
func handleTasksList(bank *tasks.Bank) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		writeJSON(w, http.StatusOK, bank.List(q.Get("stack"), q.Get("grade")))
	}
}

// runRequest — тело POST /runs (ARCHITECTURE.md §4.4).
type runRequest struct {
	Stack  string            `json:"stack"` // go | python
	Action string            `json:"action"`
	Files  map[string]string `json:"files"`
	TaskID string            `json:"task_id"`
}

// handleRun — POST /api/v1/sessions/{id}/runs → {exit_code, stdout, stderr, duration_ms, passed, tests[]}.
func handleRun(mode string, bank *tasks.Bank, rn *runner.Runner, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.PathValue("id")
		var req runRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errBody("invalid_request", "ожидается JSON {stack, action, files}"))
			return
		}
		if req.Action == "" {
			req.Action = "test"
		}
		if req.Action != "test" {
			writeJSON(w, http.StatusBadRequest, errBody("unsupported_action", "MVP: только action=test"))
			return
		}
		files := req.Files
		if req.TaskID != "" {
			task, ok := bank.Get(req.TaskID)
			if !ok {
				writeJSON(w, http.StatusBadRequest, errBody("unknown_task", "задача не найдена в банке: "+req.TaskID))
				return
			}
			// Доверенность тестов (TEST_PLAN §4.4): кандидату видны тесты задачи
			// (MVP-ограничение), но исполняются ТОЛЬКО тесты банка — «трояны»
			// в поданных *_test.go / test_*.py игнорируются (замена на банк).
			merged := make(map[string]string, len(files)+len(task.Files))
			for k, v := range files {
				merged[k] = v
			}
			for name, content := range task.Files {
				if isTaskTestFile(name, task.Stack) {
					merged[name] = content
				}
			}
			files = merged
			log.Info("run: тесты подставлены из банка (доверенность тестов)",
				"session", sessionID, "task", task.ID)
		}
		log.Info("run", "session", sessionID, "stack", req.Stack, "task", req.TaskID, "mode", mode)
		res, err := rn.Run(r.Context(), req.Stack, files)
		if err != nil {
			switch {
			case errors.Is(err, runner.ErrUnsupportedStack),
				errors.Is(err, runner.ErrBadFiles):
				writeJSON(w, http.StatusBadRequest, errBody("invalid_request", err.Error()))
			case errors.Is(err, runner.ErrNoDocker):
				writeJSON(w, http.StatusServiceUnavailable, errBody("sandbox_unavailable",
					"docker недоступен на узле (SANDBOX_MODE=docker)"))
			default:
				log.Error("run", "session", sessionID, "err", err)
				writeJSON(w, http.StatusBadGateway, errBody("sandbox_error", "ошибка запуска кода"))
			}
			return
		}
		writeJSON(w, http.StatusOK, res)
	}
}

// isTaskTestFile — имя файла с тестами задачи (go: *_test.go, python: test_*.py / *_test.py).
func isTaskTestFile(name, stack string) bool {
	switch stack {
	case "go":
		return strings.HasSuffix(name, "_test.go")
	case "python":
		base := path.Base(name)
		return strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py")
	default:
		return false
	}
}

func errBody(code, msg string) map[string]string {
	return map[string]string{"code": code, "message": msg}
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	addr := getenv("ADDR", ":8200")
	mode := getenv("SANDBOX_MODE", "subprocess") // dev-по-умолчанию: без docker-демона (prod: docker, compose)

	bank, err := tasks.Default()
	if err != nil {
		logger.Error("tasks", "err", err)
		os.Exit(1)
	}
	rn := runner.New(runner.Config{Mode: mode})

	srv := &http.Server{
		Addr:              addr,
		Handler:           withLogging(buildMux(mode, bank, rn, logger), logger),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("sandbox started", "addr", addr, "mode", mode, "tasks", bank.Count())
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("listen", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	logger.Info("sandbox stopped")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func withLogging(next http.Handler, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		status := http.StatusOK
		rec := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.written {
			status = rec.code
		}
		log.Info("http", "method", r.Method, "path", r.URL.Path,
			"status", status, "dur_ms", time.Since(start).Milliseconds())
	})
}

type statusWriter struct {
	http.ResponseWriter
	written bool
	code    int
}

func (s *statusWriter) WriteHeader(code int) {
	if !s.written {
		s.code = code
		s.written = true
	}
	s.ResponseWriter.WriteHeader(code)
}
