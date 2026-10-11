package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// Admin CRUD interview_templates (итерация 11). Все эндпоинты — за requireAdmin
// (см. Handler). DTO — db.Template (с json-тегами) как есть.

// adminTemplateReq — тело POST/PUT шаблона.
type adminTemplateReq struct {
	Name      string             `json:"name"`
	Grade     models.Grade       `json:"grade"`
	Stack     string             `json:"stack"`
	DurationS int                `json:"duration_s"`
	IsDefault bool               `json:"is_default"`
	Blocks    []db.TemplateBlock `json:"blocks"`
}

// handleAdminTemplates — GET /api/v1/admin/templates: список ВСЕХ шаблонов.
func (s *Server) handleAdminTemplates(w http.ResponseWriter, r *http.Request) {
	all, err := s.templates.ListAll(r.Context())
	if err != nil {
		s.log.Error("admin templates list", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "ошибка чтения шаблонов")
		return
	}
	if all == nil {
		all = []*db.Template{}
	}
	writeJSON(w, http.StatusOK, all)
}

// handleAdminTemplateCreate — POST /api/v1/admin/templates: новый шаблон.
func (s *Server) handleAdminTemplateCreate(w http.ResponseWriter, r *http.Request) {
	var req adminTemplateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "некорректное JSON-тело")
		return
	}
	if msg := validateAdminTemplate(&req); msg != "" {
		writeError(w, http.StatusBadRequest, "validation", msg)
		return
	}
	t := &db.Template{
		Name:      req.Name,
		Grade:     req.Grade,
		Stack:     req.Stack,
		DurationS: req.DurationS,
		Blocks:    req.Blocks,
		IsDefault: req.IsDefault,
	}
	if err := s.templates.Create(r.Context(), t); err != nil {
		s.log.Error("admin template create", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "ошибка создания шаблона")
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

// handleAdminTemplateUpdate — PUT /api/v1/admin/templates/{id}: изменение.
func (s *Server) handleAdminTemplateUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_id", "некорректный id шаблона")
		return
	}
	var req adminTemplateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "некорректное JSON-тело")
		return
	}
	if msg := validateAdminTemplate(&req); msg != "" {
		writeError(w, http.StatusBadRequest, "validation", msg)
		return
	}
	existing, err := s.templates.Get(r.Context(), id)
	if err != nil {
		s.log.Error("admin template get", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "ошибка чтения шаблона")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "not_found", "шаблон не найден")
		return
	}
	t := &db.Template{
		ID:        id,
		Name:      req.Name,
		Grade:     req.Grade,
		Stack:     req.Stack,
		DurationS: req.DurationS,
		Blocks:    req.Blocks,
		IsDefault: req.IsDefault,
	}
	if err := s.templates.Update(r.Context(), t); err != nil {
		s.log.Error("admin template update", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "ошибка обновления шаблона")
		return
	}
	updated, err := s.templates.Get(r.Context(), id)
	if err != nil {
		s.log.Error("admin template reget", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "ошибка чтения шаблона")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// handleAdminTemplateDelete — DELETE /api/v1/admin/templates/{id}: удаление.
// 409 — если это единственный дефолт грейда (оставит грейд без дефолта).
func (s *Server) handleAdminTemplateDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_id", "некорректный id шаблона")
		return
	}
	tpl, err := s.templates.Get(r.Context(), id)
	if err != nil {
		s.log.Error("admin template get", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "ошибка чтения шаблона")
		return
	}
	if tpl == nil {
		writeError(w, http.StatusNotFound, "not_found", "шаблон не найден")
		return
	}
	if tpl.IsDefault {
		n, err := s.templates.CountDefaults(r.Context(), tpl.Grade)
		if err != nil {
			s.log.Error("admin template count defaults", "err", err)
			writeError(w, http.StatusInternalServerError, "internal", "ошибка проверки дефолтов")
			return
		}
		if n <= 1 {
			writeError(w, http.StatusConflict, "last_default",
				"нельзя удалить единственный дефолт грейда "+string(tpl.Grade))
			return
		}
	}
	if err := s.templates.Delete(r.Context(), id); err != nil {
		s.log.Error("admin template delete", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "ошибка удаления шаблона")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// validateAdminTemplate — общие проверки тела шаблона (итерация 11).
func validateAdminTemplate(req *adminTemplateReq) string {
	if req.Name == "" {
		return "имя шаблона не может быть пустым"
	}
	if !req.Grade.Valid() {
		return "некорректный грейд: " + string(req.Grade)
	}
	if req.Stack == "" {
		req.Stack = "*"
	}
	if len(req.Blocks) == 0 {
		return "шаблон должен иметь хотя бы один блок программы"
	}
	for i, b := range req.Blocks {
		if b.Title == "" || b.Focus == "" {
			return "блок " + strconv.Itoa(i+1) + ": название и фокус не могут быть пустыми"
		}
		if b.QuestionCount <= 0 {
			b.QuestionCount = 1
			req.Blocks[i].QuestionCount = 1
		}
	}
	return ""
}
