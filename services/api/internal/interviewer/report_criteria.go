// Критерии и веса отчёта: таблица по грейду/треку, рекомендация по грэйду,
// итог по критериям и округление (round2).
package interviewer

import (
	"strconv"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// criteriaFor — критерии отчёта (spec-18/Ф2): из трека, при nil — хардкод
// по грейду (исторический сид весов, до ролевого движка) для сессий без трека/движка без
// хранилища. Веса трека нормализованы при валидации.
func criteriaFor(sess models.Session, track *models.Track) []CriterionScore {
	if track == nil {
		return criteriaForGrade(sess.Grade)
	}
	out := make([]CriterionScore, 0, len(track.Criteria))
	for _, c := range track.Criteria {
		out = append(out, CriterionScore{Name: c.Name, Weight: c.Weight, Score: 3})
	}
	return out
}

// criteriaForGrade — критерии и веса по грейду (исторический сид, до ролевого движка).
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

// gradeRecommendation — рекомендация погрэй (пороги §12; confirm — порог
// подтверждения, 0 → глобальный 4.2; spec-18: трек может переопределить).
func gradeRecommendation(overall, confirm float64) string {
	if confirm <= 0 {
		confirm = 4.2
	}
	switch {
	case overall >= confirm:
		return "грейд подтверждён с запасом"
	case overall >= 3.5:
		return "грейд подтверждён, есть точки роста"
	case overall >= 2.5:
		return "разрыв до грейда: рекомендуется подготовка (3–12 мес)"
	default:
		return "ниже грейда"
	}
}

// overallOf — итог по критериям: средневзвешенное (round2). Одна формула на
// parser и на finish, где итог пересчитывается после потолка по измерению.
func overallOf(crit []CriterionScore) (float64, bool) {
	overall, weightSum := 0.0, 0.0
	for _, c := range crit {
		overall += c.Score * c.Weight
		weightSum += c.Weight
	}
	if weightSum <= 0 {
		return 0, false
	}
	return round2(overall / weightSum), true
}

// formatScore — число в коротком виде для подписи в комментарии.
func formatScore(f float64) string {
	return strconv.FormatFloat(f, 'f', 1, 64)
}
func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}
