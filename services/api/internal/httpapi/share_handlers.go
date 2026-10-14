// F1-export (spec-16): публичная ссылка на отчёт.
// POST /api/v1/sessions/{id}/share — создать/переиспользовать токен (владелец).
// DELETE — отозвать. GET /api/v1/public/reports/{token} — публичный отчёт
// (без аутентификации, без персональных данных пользователя).
package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// generateShareToken — 24 случайных байта → 48 hex-символов (192 бита энтропии:
// перебор нереален; токен живёт, пока владелец не отзовёт).
func generateShareToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// handleSessionShare — POST /api/v1/sessions/{id}/share.
// Идемпотентно: существующий токен переиспользуется (ссылка стабильна).
func (s *Server) handleSessionShare(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSessionID(w, r)
	if !ok {
		return
	}
	m, ok := s.ownedSession(w, r, id)
	if !ok {
		return
	}
	if m.Status != models.StatusFinished && m.Status != models.StatusAborted {
		writeError(w, http.StatusConflict, "invalid_state", "ссылка доступна после завершения сессии")
		return
	}
	// Отчёт должен существовать — иначе ссылка ведёт в пустоту.
	if err := s.reports.Has(r.Context(), id); err != nil {
		if errors.Is(err, db.ErrReportNotFound) {
			writeError(w, http.StatusConflict, "report_not_ready", "отчёт ещё генерируется — попробуйте позже")
			return
		}
		s.log.Error("share: report check", "session", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "ошибка создания ссылки")
		return
	}
	token := m.ShareToken
	if token == "" {
		t, err := generateShareToken()
		if err != nil {
			s.log.Error("share: token generate", "session", id, "err", err)
			writeError(w, http.StatusInternalServerError, "internal", "ошибка создания ссылки")
			return
		}
		if err := s.sessions.SetShareToken(r.Context(), id, m.UserID, t); err != nil {
			s.log.Error("share: token save", "session", id, "err", err)
			writeError(w, http.StatusInternalServerError, "internal", "ошибка создания ссылки")
			return
		}
		token = t
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "path": "/share/" + token})
}

// handleSessionShareRevoke — DELETE /api/v1/sessions/{id}/share.
func (s *Server) handleSessionShareRevoke(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSessionID(w, r)
	if !ok {
		return
	}
	m, ok := s.ownedSession(w, r, id)
	if !ok {
		return
	}
	if err := s.sessions.SetShareToken(r.Context(), id, m.UserID, ""); err != nil {
		s.log.Error("share: revoke", "session", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "ошибка отзыва ссылки")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revoked": true})
}

// handlePublicReport — GET /api/v1/public/reports/{token} (без auth).
// Возвращает отчёт и метаданные сессии (грейд/стек/дата) — без email и ID
// пользователя (персональные данные наружу не выдаём).
func (s *Server) handlePublicReport(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if len(token) < 16 {
		writeError(w, http.StatusNotFound, "not_found", "ссылка не найдена")
		return
	}
	m, err := s.sessions.GetByShareToken(r.Context(), token)
	if errors.Is(err, db.ErrSessionNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "ссылка не найдена")
		return
	}
	if err != nil {
		s.log.Error("public report: lookup", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "ошибка получения отчёта")
		return
	}
	if m.Status != models.StatusFinished && m.Status != models.StatusAborted {
		writeError(w, http.StatusNotFound, "not_found", "ссылка не найдена")
		return
	}
	overall, gradeRec, criteria, strengths, weaknesses, recommendations, extra, err :=
		s.reports.Get(r.Context(), m.ID)
	if errors.Is(err, db.ErrReportNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "ссылка не найдена")
		return
	}
	if err != nil {
		s.log.Error("public report: get", "session", m.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "ошибка получения отчёта")
		return
	}
	meta := map[string]any{"grade": m.Grade, "stack": m.Stack}
	if m.FinishedAt != nil {
		meta["finished_at"] = m.FinishedAt.UTC().Format("2006-01-02")
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"meta":                 meta,
		"overall":              overall,
		"grade_recommendation": gradeRec,
		"criteria":             json.RawMessage(criteria),
		"strengths":            json.RawMessage(strengths),
		"weaknesses":           json.RawMessage(weaknesses),
		"recommendations":      json.RawMessage(recommendations),
		"verdict":              extra.Verdict,
		"grade_gap":            extra.GradeGap,
		"study_plan_2weeks":    json.RawMessage(orJSONArray(extra.StudyPlan2Weeks)),
		"progress_vs_previous": json.RawMessage(orJSONArray(extra.ProgressVsPrevious)),
		"livecode_tasks":       json.RawMessage(orJSONArray(extra.LiveCodeTasks)),
	})
}
