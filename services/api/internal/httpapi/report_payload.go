package httpapi

// Тело отчёта: один конструктор для приватного GET /sessions/{id}/report и
// публичной ссылки /share/{token}. Раньше оба обработчика собирали набор
// ключей каждый сам, и он разъехался: пустые поля приватного пути отдавались
// как null, публичного — как []. Для фронта это не косметика: ReportView
// вызывает .map на criteria/strengths/weaknesses/recommendations и на null
// падает.

import (
	"encoding/json"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
)

// reportPayload — JSON отчёта. Массивы, пришедшие из БД пустыми (строки
// старых прогонов), разворачиваются в [], а не в null.
func reportPayload(overall float64, gradeRecommendation string,
	criteria, strengths, weaknesses, recommendations []byte, extra db.ReportExtra) map[string]any {
	return map[string]any{
		"overall":              overall,
		"grade_recommendation": gradeRecommendation,
		// json.RawMessage — встроить JSON-массивы как есть, а не base64-строкой.
		"criteria":        json.RawMessage(orJSONArray(string(criteria))),
		"strengths":       json.RawMessage(orJSONArray(string(strengths))),
		"weaknesses":      json.RawMessage(orJSONArray(string(weaknesses))),
		"recommendations": json.RawMessage(orJSONArray(string(recommendations))),
		// Подробный отчёт (Итерация B).
		"verdict":   extra.Verdict,
		"grade_gap": extra.GradeGap,
		// Путь по уровням вопросов (собеседовали на шаг выше резюме).
		"question_trail":       extra.QuestionTrail,
		"study_plan_2weeks":    json.RawMessage(orJSONArray(extra.StudyPlan2Weeks)),
		"progress_vs_previous": json.RawMessage(orJSONArray(extra.ProgressVsPrevious)),
		// Подробно по задачам Live-Code (spec-13B T3); старые строки — [].
		"livecode_tasks": json.RawMessage(orJSONArray(extra.LiveCodeTasks)),
		// Оценки по стадиям (spec-24): детерминированные числа, поверх которых
		// построено суждение оценщика; экран отчёта показывает их как «как считалось».
		"stages": json.RawMessage(orJSONArray(extra.Stages)),
	}
}

// orJSONArray — пустой/отсутствующий JSON-массив → "[]" (старые строки отчёта).
func orJSONArray(raw string) string {
	if raw == "" {
		return "[]"
	}
	return raw
}
