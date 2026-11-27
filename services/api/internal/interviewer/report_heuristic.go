// Детерминированный отчёт без LLM (fallback): оценки по стадиям, прогресс vs
// прошлые интервью, per-task результаты Live-Code по событиям сессии.
package interviewer

import (
	"context"
	"encoding/json"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// heuristicReport — детерминированный отчёт (fallback, когда LLM не вернул JSON:
// mock-режим, ошибки парсинга). Шкала 1–5 по данным сессии.
func heuristicReport(sess models.Session, mat reportMaterial, track *models.Track) ReportData {
	crit := criteriaFor(sess, track)
	confirm := 0.0
	if track != nil {
		confirm = track.GradeConfirm
	}
	// spec-24: критерии берутся из оценок стадий. Раньше Live-Code давал 4 и за
	// «решил одну задачу из трёх», и за «решил все».
	stages := stageReports(mat, track)
	for i := range crit {
		src := measuredStage(crit[i].Name, stages)
		if src != nil {
			crit[i].Score = src.Score
			crit[i].Comment = src.Title + ": " + src.Basis
		} else {
			// Критерий не про измеренную стадию (фундамент, leadership): на
			// авто-отчёте — средняя оценка уровня, рассудит LLM-оценщик.
			crit[i].Score = 3
			crit[i].Comment = "нет данных стадии по этому критерию (авто)"
		}
		crit[i].GapToGrade = round2(3 - crit[i].Score)
	}
	overall := 0.0
	for _, c := range crit {
		overall += c.Score * c.Weight
	}
	overall = round2(overall)
	report := ReportData{
		Overall:             overall,
		GradeRecommendation: gradeRecommendation(overall, confirm),
		Criteria:            crit,
		Stages:              stages,
		Strengths:           stageStrengths(stages),
		Weaknesses:          stageWeaknesses(stages),
		Recommendations:     []string{"повторное интервью с акцентом на слабые критерии"},
		// Итерация B: LLM-полей нет (fallback — базовые/пустые).
		Verdict:            "Детальный разбор — после подключения LLM-оценщика",
		GradeGap:           "—",
		StudyPlan2Weeks:    []string{},
		ProgressVsPrevious: []ProgressEntry{},
	}
	return report
}

// previousReports — последние 5 завершённых интервью пользователя (кроме
// текущей сессии) с сохранёнными отчётами (Итерация B, для progress).
type previousSession struct {
	SessionID int64            `json:"session_id"`
	Date      string           `json:"date"` // RFC3339
	Stack     string           `json:"stack"`
	Grade     string           `json:"grade"`
	Overall   float64          `json:"overall"`
	Criteria  []CriterionScore `json:"criteria"`
}

const maxPreviousForReport = 5

func (i *Interviewer) previousReports(ctx context.Context, sess models.Session) []previousSession {
	if i.reports == nil {
		return nil
	}
	list, err := i.sessions.ListByUser(ctx, sess.UserID)
	if err != nil {
		i.log.Debug("report: прошлые сессии не прочитаны", "user", sess.UserID, "err", err)
		return nil
	}
	var out []previousSession
	for _, m := range list {
		if m.ID == sess.ID {
			continue
		}
		if m.Status != models.StatusFinished && m.Status != models.StatusAborted {
			continue
		}
		overall, _, critJSON, _, _, _, _, err := i.reports.Get(ctx, m.ID)
		if err != nil {
			continue // отчёт ещё не сгенерирован — пропускаем
		}
		var crit []CriterionScore
		if len(critJSON) > 0 {
			_ = json.Unmarshal(critJSON, &crit)
		}
		out = append(out, previousSession{
			SessionID: m.ID,
			Date:      m.StartedAt.UTC().Format(time.RFC3339),
			Stack:     string(m.Stack),
			Grade:     string(m.Grade),
			Overall:   overall,
			Criteria:  crit,
		})
		if len(out) >= maxPreviousForReport {
			break
		}
	}
	return out
}

// buildProgress — детерминированный прогресс vs прошлые (Итерация B):
// delta = current − previous по совпадающим критериям.
func buildProgress(sess models.Session, current []CriterionScore, prev []previousSession) []ProgressEntry {
	cur := map[string]float64{}
	for _, c := range current {
		cur[c.Name] = c.Score
	}
	out := make([]ProgressEntry, 0, len(prev))
	for _, p := range prev {
		delta := map[string]float64{}
		for _, c := range p.Criteria {
			s, ok := cur[c.Name]
			if ok {
				delta[c.Name] = round2(s - c.Score)
			}
		}
		out = append(out, ProgressEntry{
			SessionID:     p.SessionID,
			Date:          p.Date,
			Stack:         p.Stack,
			Grade:         p.Grade,
			Overall:       p.Overall,
			SameStack:     p.Stack == string(sess.Stack),
			CriteriaDelta: delta,
		})
	}
	return out
}

// liveCodeTaskResults — per-task статусы (spec-13B T3): последняя сдача задачи
// (code_run: task_id+passed) → passed/failed; сдач нет → no_submission.
func liveCodeTaskResults(mat reportMaterial) []LiveCodeTaskResult {
	last := map[string]bool{} // task_id → passed последней сдачи
	for _, r := range mat.CodeRuns {
		id, _ := r["task_id"].(string)
		if id == "" {
			continue
		}
		p, _ := r["passed"].(bool)
		last[id] = p
	}
	out := make([]LiveCodeTaskResult, 0, len(mat.LiveCodeTasks))
	for _, t := range mat.LiveCodeTasks {
		status := "no_submission"
		if p, ok := last[t.ID]; ok {
			if p {
				status = "passed"
			} else {
				status = "failed"
			}
		}
		out = append(out, LiveCodeTaskResult{ID: t.ID, Title: t.Title, Status: status})
	}
	return out
}
