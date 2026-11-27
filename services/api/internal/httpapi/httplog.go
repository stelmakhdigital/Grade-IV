// Запись HTTP-ответов и лог запросов: формат ошибки, writeJSON/writeError,
// statusRecorder (с Hijack для WS-апгрейда) и middleware withLogging.

package httpapi

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"time"
)

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
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		status := rec.status
		if status == 0 {
			// Статус не записан: WS-апгрейд пишет 101 прямо в hijack-коннект.
			status = http.StatusOK
		}
		s.log.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"dur_ms", time.Since(start).Milliseconds(),
		)
	})
}
