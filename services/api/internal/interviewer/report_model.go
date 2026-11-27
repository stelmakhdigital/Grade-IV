// Модель отчёта: типы данных (критерии, итог, прогресс, Live-Code) и их JSON-форма.
package interviewer

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
	// QuestionTrail — путь по уровням вопросов: где кандидат держал цель, где
	// интервьюер снижал сложность. Пусто — адаптивного уровня не было.
	QuestionTrail string `json:"question_trail,omitempty"`
	// ProgressVsPrevious — все прошлые интервью (последние 5), Итерация B.
	ProgressVsPrevious []ProgressEntry `json:"progress_vs_previous"`
	// LiveCodeTasks — per-task результаты Live-Code (spec-13B T3), детерминированно.
	LiveCodeTasks []LiveCodeTaskResult `json:"livecode_tasks"`
	// Stages — оценки по стадиям (spec-24, инкремент 4): число, которое можно
	// пересчитать по событиям сессии. Лежат под суждением LLM-оценщика, а не
	// вместо него.
	Stages []StageReport `json:"stages"`
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

// LiveCodeTaskRef — задача из события livecode_tasks (выбор при входе на стадию).
type LiveCodeTaskRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// LiveCodeTaskResult — задача сессии в отчёте (spec-13B T3): название + результат.
// Status: passed | failed | no_submission.
type LiveCodeTaskResult struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}
