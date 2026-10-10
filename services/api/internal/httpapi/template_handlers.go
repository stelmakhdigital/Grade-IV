package httpapi

import (
	"net/http"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// handleTemplatesList — GET /api/v1/templates?grade=junior&stack=go
// Список шаблонов для грейда/стека (дефолтные первыми). 400 — без grade.
func (s *Server) handleTemplatesList(w http.ResponseWriter, r *http.Request) {
	grade := models.Grade(r.URL.Query().Get("grade"))
	if !grade.Valid() {
		writeError(w, http.StatusBadRequest, "invalid_request", "ожидается ?grade=junior|middle|senior|staff")
		return
	}
	stack := r.URL.Query().Get("stack")
	if stack == "" {
		stack = "go"
	}
	tpls, err := s.templates.List(r.Context(), grade, stack)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "ошибка БД")
		return
	}
	out := make([]map[string]any, 0, len(tpls))
	for _, t := range tpls {
		out = append(out, map[string]any{
			"id":          t.ID,
			"name":        t.Name,
			"grade":       t.Grade,
			"stack":       t.Stack,
			"duration_s":  t.DurationS,
			"blocks":      t.Blocks,
			"is_default":  t.IsDefault,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": out})
}
