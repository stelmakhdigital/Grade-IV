// Dev-эндпоинт /debug/mic-report: приём отчёта диагностики микрофона из
// браузера (audio-debug.html) в FOR_RUN/debug/. Отдельный debug-only хендлер.

package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"time"
)

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
