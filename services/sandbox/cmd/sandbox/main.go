// Command sandbox — сандбокс выполнения кода кандидата (ARCHITECTURE.md §4.4, ADR-003).
// WP-1: health + заглушка /runs (501). WP-6: Docker-runner (лимиты, --network=none)
// и dev-fallback subprocess.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// buildMux — маршруты сервиса (вынесено для тестов).
func buildMux(mode string, log *slog.Logger) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"service": "grade-sandbox",
			"mode":    mode,
		})
	})
	mux.HandleFunc("POST /api/v1/sessions/{id}/runs", func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.PathValue("id")
		log.Info("runs: ожидает реализации (WP-6)", "session", sessionID)
		writeJSON(w, http.StatusNotImplemented, map[string]string{
			"code":    "not_implemented",
			"message": "sandbox runner — реализация в WP-6",
		})
	})
	return mux
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	addr := getenv("ADDR", ":8200")
	mode := getenv("SANDBOX_MODE", "docker")

	srv := &http.Server{
		Addr:              addr,
		Handler:           withLogging(buildMux(mode, logger), logger),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("sandbox started", "addr", addr, "mode", mode)
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
