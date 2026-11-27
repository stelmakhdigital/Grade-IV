package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
)

// Чтение журнала событий сессии: транскрипт/история (FR-A4).

// handleSessionEvents — GET /api/v1/sessions/{id}/events (транскрипт/история, FR-A4).
func (s *Server) handleSessionEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSessionID(w, r)
	if !ok {
		return
	}
	m, err := s.sessions.GetOwned(r.Context(), id, userIDFromContext(r.Context()))
	if errors.Is(err, db.ErrSessionNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "сессия не найдена")
		return
	}
	events, err := s.sessions.ListEvents(r.Context(), m.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "не удалось получить события")
		return
	}
	type eventDTO struct {
		Seq  int             `json:"seq"`
		TS   time.Time       `json:"ts"`
		Kind string          `json:"kind"`
		Data json.RawMessage `json:"data"`
	}
	out := make([]eventDTO, 0, len(events))
	for _, e := range events {
		out = append(out, eventDTO{Seq: e.Seq, TS: e.TS, Kind: e.Kind, Data: json.RawMessage(e.Data)})
	}
	writeJSON(w, http.StatusOK, out)
}
