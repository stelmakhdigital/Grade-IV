package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/llm"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// Отчёт сессии (WP-11): REST-ручка GET /sessions/{id}/report и fire-and-forget
// запуск генерации.

// handleReportGet — GET /api/v1/sessions/{id}/report (WP-11).
// 200 — отчёт; 202 — сессия завершена, отчёт генерируется; 409 — сессия не завершена.
func (s *Server) handleReportGet(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSessionID(w, r)
	if !ok {
		return
	}
	m, ok := s.ownedSession(w, r, id)
	if !ok {
		return
	}
	if m.Status != models.StatusFinished && m.Status != models.StatusAborted {
		writeError(w, http.StatusConflict, "invalid_state", "отчёт доступен после завершения сессии")
		return
	}
	overall, gradeRec, criteria, strengths, weaknesses, recommendations, extra, err :=
		s.reports.Get(r.Context(), id)
	if errors.Is(err, db.ErrReportNotFound) {
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "generating"})
		return
	}
	if err != nil {
		s.log.Error("report get", "session", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "ошибка получения отчёта")
		return
	}
	writeJSON(w, http.StatusOK, reportPayload(overall, gradeRec, criteria, strengths,
		weaknesses, recommendations, extra))
}

// startReportGeneration — fire-and-forget генерация отчёта (WP-11):
// LLM-оценщик (fallback — детерминированный), сохранение, report_ready по WS.
func (s *Server) startReportGeneration(id int64) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*llm.DefaultTimeout)
		defer cancel()
		data, err := s.interviewer.GenerateReport(ctx, id)
		if err != nil {
			s.log.Warn("report: генерация не удалась", "session", id, "err", err)
			return
		}
		critJSON, _ := json.Marshal(data.Criteria)
		strJSON, _ := json.Marshal(data.Strengths)
		weakJSON, _ := json.Marshal(data.Weaknesses)
		recJSON, _ := json.Marshal(data.Recommendations)
		sp2wJSON, _ := json.Marshal(data.StudyPlan2Weeks)
		progJSON, _ := json.Marshal(data.ProgressVsPrevious)
		lctJSON, _ := json.Marshal(data.LiveCodeTasks)
		stagesJSON, _ := json.Marshal(data.Stages)
		if err := s.reports.Save(ctx, id, data.Overall, data.GradeRecommendation,
			critJSON, strJSON, weakJSON, recJSON, db.ReportExtra{
				Verdict:            data.Verdict,
				GradeGap:           data.GradeGap,
				QuestionTrail:      data.QuestionTrail,
				StudyPlan2Weeks:    string(sp2wJSON),
				ProgressVsPrevious: string(progJSON),
				LiveCodeTasks:      string(lctJSON),
				Stages:             string(stagesJSON),
			}); err != nil {
			s.log.Error("report save", "session", id, "err", err)
			return
		}
		s.log.Info("отчёт готов", "session", id, "overall", data.Overall)
		s.engine.SendTo(id, map[string]any{"type": "report_ready", "overall": data.Overall})
	}()
}
