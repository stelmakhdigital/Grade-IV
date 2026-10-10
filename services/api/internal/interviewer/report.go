package interviewer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/llm"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// CriterionScore — оценка по одному критерию (шкала 1–5, §12).
// Итерация B: evidence (примеры из сессии), gap до грейда, план обучения.
type CriterionScore struct {
	Name    string  `json:"name"`
	Weight  float64 `json:"weight"` // доля, 0..1
	Score   float64 `json:"score"`  // 1..5
	Comment string  `json:"comment,omitempty"`
	// Evidence — 2–3 примера из сессии (транскрипт/код), Итерация B.
	Evidence []string `json:"evidence,omitempty"`
	// GapToGrade — benchmark (3 = соответствует грейду) минус score, Итерация B.
	GapToGrade float64  `json:"gap_to_grade,omitempty"`
	StudyPlan  []string `json:"study_plan,omitempty"` // 2–3 пункта, Итерация B
}

// ReportData — структурированный отчёт (критерии + тексты).
// Итерация B: вердикт, план на 2 недели, разрыв до грейда, прогресс.
type ReportData struct {
	Overall             float64          `json:"overall"`
	GradeRecommendation string           `json:"grade_recommendation"`
	Verdict             string           `json:"verdict"` // 1–2 предложения, Итерация B
	Criteria            []CriterionScore `json:"criteria"`
	Strengths           []string         `json:"strengths"`
	Weaknesses          []string         `json:"weaknesses"`
	Recommendations     []string         `json:"recommendations"`
	// StudyPlan2Weeks — 10–15 пунктов на 2 недели (приоритет: gap × weight), Итерация B.
	StudyPlan2Weeks []string `json:"study_plan_2weeks"`
	// GradeGap — «До X не хватает Y», Итерация B.
	GradeGap string `json:"grade_gap"`
	// ProgressVsPrevious — все прошлые интервью (последние 5), Итерация B.
	ProgressVsPrevious []ProgressEntry `json:"progress_vs_previous"`
}

// ProgressEntry — прошлое интервью для сравнения прогресса (Итерация B).
type ProgressEntry struct {
	SessionID     int64              `json:"session_id"`
	Date          string             `json:"date"` // RFC3339
	Stack         string             `json:"stack"`
	Grade         string             `json:"grade"`
	Overall       float64            `json:"overall"`
	SameStack     bool               `json:"same_stack"`     // для UI: выделить
	CriteriaDelta map[string]float64 `json:"criteria_delta"` // {критерий: delta}
}

// criteriaForGrade — критерии и веса по грейду (REQUIREMENTS.md §12, v1.1).
func criteriaForGrade(grade models.Grade) []CriterionScore {
	type def struct {
		name   string
		weight float64
	}
	byGrade := map[models.Grade][]def{
		models.GradeJunior: {
			{"CS-фундамент (ООП, структуры данных, сложность)", 0.40},
			{"Live-Code (алгоритм, качество кода, тесты)", 0.30},
			{"Поведенческое (проекты, конфликты, ownership)", 0.10},
			{"Коммуникация (ясность, структура, русский язык)", 0.20},
		},
		models.GradeMiddle: {
			{"CS-фундамент (ООП, структуры данных, сложность)", 0.20},
			{"Deep-dive по стеку (Go/Python)", 0.25},
			{"Live-Code (алгоритм, качество кода, тесты)", 0.25},
			{"System Design (сервис с нуля, масштабируемость)", 0.15},
			{"Коммуникация (ясность, структура, русский язык)", 0.15},
		},
		models.GradeSenior: {
			{"Deep-dive по стеку (Go/Python)", 0.25},
			{"Live-Code (алгоритм, качество кода, тесты)", 0.10},
			{"System Design (сервис с нуля, масштабируемость)", 0.30},
			{"Кросс-системный дизайн и масштаб (×10, цена масштабирования)", 0.20},
			{"Leadership и наставничество (влияние, найм, стратегия)", 0.05},
			{"Коммуникация (ясность, структура, русский язык)", 0.10},
		},
		models.GradeStaff: {
			{"Deep-dive по стеку (Go/Python)", 0.05},
			{"Кросс-системный дизайн и масштаб (×10, цена масштабирования)", 0.35},
			{"Архитектурные компромиссы (деньги/скорость/риск)", 0.25},
			{"Leadership и наставничество (влияние, найм, стратегия)", 0.20},
			{"Коммуникация (ясность, структура, русский язык)", 0.15},
		},
	}
	defs := byGrade[grade]
	if len(defs) == 0 {
		defs = byGrade[models.GradeMiddle]
	}
	out := make([]CriterionScore, 0, len(defs))
	for _, d := range defs {
		out = append(out, CriterionScore{Name: d.name, Weight: d.weight, Score: 3})
	}
	return out
}

// gradeRecommendation — рекомендация погрэй (пороги §12).
func gradeRecommendation(overall float64) string {
	switch {
	case overall >= 4.2:
		return "грейд подтверждён с запасом"
	case overall >= 3.5:
		return "грейд подтверждён, есть точки роста"
	case overall >= 2.5:
		return "разрыв до грейда: рекомендуется подготовка (3–12 мес)"
	default:
		return "ниже грейда"
	}
}

// reportMaterial — данные сессии для отчёта (транскрипт, кодовые запуски, схема).
type reportMaterial struct {
	Grade      string
	Stack      string
	Transcript []string
	CodeRuns   []map[string]any
	Design     map[string]any
}

// reportMaterialFromEvents — сбор материала из событий сессии (Data — JSON).
func (i *Interviewer) reportMaterialFromEvents(ctx context.Context, sessionID int64) (reportMaterial, error) {
	events, err := i.sessions.ListEvents(ctx, sessionID)
	if err != nil {
		return reportMaterial{}, err
	}
	var mat reportMaterial
	for _, ev := range events {
		var data map[string]any
		if len(ev.Data) > 0 {
			_ = json.Unmarshal(ev.Data, &data)
		}
		switch ev.Kind {
		case "user_utterance", "ai_utterance", "ai_nudge":
			if s, ok := data["text"].(string); ok && s != "" {
				who := "кандидат"
				if ev.Kind != "user_utterance" {
					who = "интервьюер"
				}
				mat.Transcript = append(mat.Transcript, who+": "+s)
			}
		case "code_run":
			if len(data) > 0 {
				mat.CodeRuns = append(mat.CodeRuns, data)
			}
		case "whiteboard_save":
			if len(data) > 0 {
				mat.Design = data
			}
		}
	}
	return mat, nil
}

// heuristicReport — детерминированный отчёт (fallback, когда LLM не вернул JSON:
// mock-режим, ошибки парсинга). Шкала 1–5 по данным сессии.
func heuristicReport(sess models.Session, mat reportMaterial) ReportData {
	crit := criteriaForGrade(sess.Grade)
	scoreOf := func(substr string, fallback float64) float64 { return fallback }
	_ = scoreOf
	// Live-Code: были проходы — 4, запуски без проходов — 3, нет запусков — 2.
	codeScore := 2.0
	passed, failed := 0, 0
	for _, r := range mat.CodeRuns {
		if p, ok := r["passed"].(bool); ok && p {
			passed++
		} else {
			failed++
		}
	}
	switch {
	case passed > 0:
		codeScore = 4
	case len(mat.CodeRuns) > 0 && failed > 0:
		codeScore = 3
	}
	// System Design: схема с блоками — 3, без — 2.
	designScore := 2.0
	if blocks, ok := mat.Design["blocks"].([]any); ok && len(blocks) > 0 {
		designScore = 3
	}
	// Коммуникация: была речь кандидата — 3, молчал — 2.
	speech := 2.0
	for _, t := range mat.Transcript {
		if strings.HasPrefix(t, "кандидат:") {
			speech = 3
			break
		}
	}
	for i := range crit {
		switch {
		case strings.Contains(crit[i].Name, "Live-Code"):
			crit[i].Score = codeScore
		case strings.Contains(crit[i].Name, "System Design"),
			strings.Contains(crit[i].Name, "Кросс-системный"),
			strings.Contains(crit[i].Name, "Архитектурные"):
			crit[i].Score = designScore
		case strings.Contains(crit[i].Name, "Коммуникация"):
			crit[i].Score = speech
		default:
			crit[i].Score = 3 // без глубинного анализа — средняя оценка уровня
		}
		crit[i].Comment = "оценка по данным сессии (авто)"
		crit[i].GapToGrade = round2(3 - crit[i].Score) // 3 — соответствует грейду
	}
	overall := 0.0
	for _, c := range crit {
		overall += c.Score * c.Weight
	}
	overall = round2(overall)
	report := ReportData{
		Overall:             overall,
		GradeRecommendation: gradeRecommendation(overall),
		Criteria:            crit,
		Strengths:           []string{fmt.Sprintf("активное участие в интервью (%d реплик)", len(mat.Transcript))},
		Weaknesses:          []string{"детальный разбор — после подключения LLM-оценщика"},
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

// GenerateReport — итоговый отчёт (WP-11 + Итерация B: подробный):
// LLM-оценщик по рубрике §12 с контекстом прошлых интервью + fallback.
// Сессия должна быть в терминальном статусе (finished/aborted).
func (i *Interviewer) GenerateReport(ctx context.Context, sessionID int64) (ReportData, error) {
	ctx, cancel := context.WithTimeout(ctx, llm.DefaultTimeout)
	defer cancel()

	sess, err := i.sessions.Get(ctx, sessionID)
	if err != nil {
		return ReportData{}, err
	}
	if sess.Status != models.StatusFinished && sess.Status != models.StatusAborted {
		return ReportData{}, fmt.Errorf("отчёт доступен после завершения сессии (статус %s)", sess.Status)
	}
	mat, err := i.reportMaterialFromEvents(ctx, sessionID)
	if err != nil {
		return ReportData{}, err
	}
	prev := i.previousReports(ctx, sess)
	finish := func(r ReportData) ReportData {
		// Прогресс — детерминированно (числовые delta не доверяем LLM).
		r.ProgressVsPrevious = buildProgress(sess, r.Criteria, prev)
		if r.StudyPlan2Weeks == nil {
			r.StudyPlan2Weeks = []string{}
		}
		if r.ProgressVsPrevious == nil {
			r.ProgressVsPrevious = []ProgressEntry{}
		}
		return r
	}

	// LLM-оценщик: строго JSON по формату.
	tr := strings.Join(mat.Transcript, "\n")
	if len(tr) > 6000 {
		tr = tr[len(tr)-6000:]
	}
	codeJSON, _ := json.Marshal(mat.CodeRuns)
	designJSON, _ := json.Marshal(mat.Design)
	critJSON, _ := json.Marshal(criteriaForGrade(sess.Grade))
	prevJSON, _ := json.Marshal(prev)
	if prevJSON == nil {
		prevJSON = []byte("[]")
	}
	userMsg := fmt.Sprintf(
		"Оцени кандидата (грейд %s, стек %s). Критерии и веса: %s. "+
			"Транскрипт:\n%s\nКодовые запуски: %s\nСхема System Design: %s\n"+
			"Прошлые интервью: %s\n"+
			"Ответь СТРОГО JSON без пояснений: {\"criteria\":[{\"name\":...,\"weight\":...,\"score\":1-5,\"comment\":...,"+
			"\"evidence\":[2-3 примера из сессии],\"gap_to_grade\":3-score,\"study_plan\":[2-3 пункта]}],"+
			"\"verdict\":1-2 предложения,\"strengths\":[...],\"weaknesses\":[...],\"recommendations\":[...],"+
			"\"study_plan_2weeks\":[10-15 пунктов, приоритет по gap × weight],"+
			"\"grade_gap\":\"До X не хватает Y\"} — тексты на русском.",
		sess.Grade, sess.Stack, string(critJSON), tr, string(codeJSON), string(designJSON), string(prevJSON))
	sys := "Ты — технический интервьюер-оценщик. Оцениваешь кандидата по критериям с весами, " +
		"шкала 1–5 (1 — не соответствует уровню грейда, 3 — соответствует, 5 — существенно выше). " +
		"Тон конструктивный, для кандидата (не для HR). Каждый критерий: evidence (2–3 примера из " +
		"транскрипта/кода), study_plan (2–3 конкретных пункта). Study plan 2 weeks: 10–15 пунктов, " +
		"приоритет по gap × weight. Progress vs previous: сравни с прошлыми интервью (все стеки, " +
		"same_stack = true для тех же стеков)."
	resp, err := i.llm.Chat(ctx, llm.Request{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: sys},
			{Role: llm.RoleUser, Content: userMsg},
		},
		Temperature: 0.3,
		MaxTokens:   2500,
	})
	if err == nil {
		if r, ok := parseReportJSON(resp.Content, sess.Grade); ok {
			return finish(r), nil
		}
	}
	h := heuristicReport(sess, mat)
	return finish(h), nil
}

// parseReportJSON — извлечение отчёта из ответа LLM (первая {...} блок).
func parseReportJSON(content string, grade models.Grade) (ReportData, bool) {
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start < 0 || end <= start {
		return ReportData{}, false
	}
	var raw struct {
		Criteria []struct {
			Name       string   `json:"name"`
			Weight     float64  `json:"weight"`
			Score      float64  `json:"score"`
			Comment    string   `json:"comment"`
			Evidence   []string `json:"evidence"`
			GapToGrade float64  `json:"gap_to_grade"`
			StudyPlan  []string `json:"study_plan"`
		} `json:"criteria"`
		Verdict         string   `json:"verdict"`
		Strengths       []string `json:"strengths"`
		Weaknesses      []string `json:"weaknesses"`
		Recommendations []string `json:"recommendations"`
		StudyPlan2Weeks []string `json:"study_plan_2weeks"`
		GradeGap        string   `json:"grade_gap"`
	}
	if err := json.Unmarshal([]byte(content[start:end+1]), &raw); err != nil || len(raw.Criteria) == 0 {
		return ReportData{}, false
	}
	// Веса из таблицы §12 (защита от «слитых» весов LLM), баллы — из ответа.
	byName := map[string]float64{}
	for _, c := range criteriaForGrade(grade) {
		byName[c.Name] = c.Weight
	}
	crit := make([]CriterionScore, 0, len(raw.Criteria))
	overall, weightSum := 0.0, 0.0
	for _, c := range raw.Criteria {
		w := c.Weight
		if ww, ok := byName[c.Name]; ok {
			w = ww
		}
		if w <= 0 {
			w = 0.1
		}
		s := c.Score
		if s < 1 {
			s = 1
		}
		if s > 5 {
			s = 5
		}
		gap := c.GapToGrade
		if gap == 0 {
			gap = round2(3 - s) // benchmark 3 = соответствует грейду
		}
		crit = append(crit, CriterionScore{
			Name: c.Name, Weight: w, Score: s, Comment: c.Comment,
			Evidence: c.Evidence, GapToGrade: gap, StudyPlan: c.StudyPlan,
		})
		overall += s * w
		weightSum += w
	}
	if weightSum <= 0 {
		return ReportData{}, false
	}
	overall = round2(overall / weightSum)
	gradeGap := raw.GradeGap
	if gradeGap == "" {
		gradeGap = "—"
	}
	return ReportData{
		Overall:             overall,
		GradeRecommendation: gradeRecommendation(overall),
		Verdict:             raw.Verdict,
		Criteria:            crit,
		Strengths:           raw.Strengths,
		Weaknesses:          raw.Weaknesses,
		Recommendations:     raw.Recommendations,
		StudyPlan2Weeks:     raw.StudyPlan2Weeks,
		GradeGap:            gradeGap,
	}, true
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}
