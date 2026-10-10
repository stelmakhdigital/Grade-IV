package interviewer

import (
	"strings"
	"testing"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// TestParseReportJSONNewFields — подробный отчёт (Итерация B): новые поля.
func TestParseReportJSONNewFields(t *testing.T) {
	content := `Оценка:
{
  "criteria": [
    {"name": "CS-фундамент (ООП, структуры данных, сложность)", "weight": 0.2, "score": 4,
     "comment": "хорошо", "evidence": ["разобрал хеш-таблицу", "O(n log n)"],
     "gap_to_grade": -1, "study_plan": ["разобрать B-деревья"]},
    {"name": "Live-Code (алгоритм, качество кода, тесты)", "weight": 0.25, "score": 3,
     "comment": "ок"}
  ],
  "verdict": "Кандидат уверенно держит уровень middle.",
  "strengths": ["ясность"],
  "weaknesses": ["системный дизайн"],
  "recommendations": ["практика SD"],
  "study_plan_2weeks": ["кэш-паттерны", "нагрузочное тестирование"],
  "grade_gap": "До senior не хватает глубины кросс-системного дизайна"
}`
	r, ok := parseReportJSON(content, models.GradeMiddle)
	if !ok {
		t.Fatal("parse: ok = false")
	}
	if r.Verdict == "" {
		t.Error("verdict пуст")
	}
	if r.GradeGap == "" || strings.Contains(r.GradeGap, "senior") == false {
		t.Errorf("grade_gap = %q", r.GradeGap)
	}
	if len(r.StudyPlan2Weeks) != 2 {
		t.Errorf("study_plan_2weeks: %d пунктов", len(r.StudyPlan2Weeks))
	}
	if len(r.Criteria) != 2 {
		t.Fatalf("criteria: %d", len(r.Criteria))
	}
	c0, c1 := r.Criteria[0], r.Criteria[1]
	if len(c0.Evidence) != 2 || len(c0.StudyPlan) != 1 {
		t.Errorf("criterion[0]: evidence=%d study_plan=%d", len(c0.Evidence), len(c0.StudyPlan))
	}
	if c0.GapToGrade != -1 {
		t.Errorf("criterion[0].gap_to_grade = %v, want -1 (из ответа LLM)", c0.GapToGrade)
	}
	// gap_to_grade не дан LLM — вычисляется: 3 - score.
	if c1.GapToGrade != 0 {
		t.Errorf("criterion[1].gap_to_grade = %v, want 0 (3-3)", c1.GapToGrade)
	}
	// Веса — из таблицы §12, не из ответа LLM.
	if c1.Weight != 0.25 {
		t.Errorf("criterion[1].weight = %v, want 0.25", c1.Weight)
	}
}

// TestParseReportJSONDefaults — отсутствие новых полей не ломает базовый формат.
func TestParseReportJSONDefaults(t *testing.T) {
	content := `{"criteria":[{"name":"CS-фундамент (ООП, структуры данных, сложность)","weight":0.2,
		"score":2,"comment":"слабо"}],"strengths":[],"weaknesses":[],"recommendations":[]}`
	r, ok := parseReportJSON(content, models.GradeMiddle)
	if !ok {
		t.Fatal("parse: ok = false")
	}
	if r.Verdict != "" {
		t.Errorf("verdict = %q, want пустой", r.Verdict)
	}
	if r.GradeGap != "—" {
		t.Errorf("grade_gap = %q, want «—»", r.GradeGap)
	}
	if r.Criteria[0].GapToGrade != 1 {
		t.Errorf("gap_to_grade = %v, want 1 (3-2)", r.Criteria[0].GapToGrade)
	}
}

// TestHeuristicReportNewFields — fallback: новые поля пустые/базовые.
func TestHeuristicReportNewFields(t *testing.T) {
	sess := models.Session{Grade: models.GradeMiddle, Stack: models.StackGo}
	mat := reportMaterial{Transcript: []string{"кандидат: привет"}}
	r := heuristicReport(sess, mat)
	if r.Verdict != "Детальный разбор — после подключения LLM-оценщика" {
		t.Errorf("verdict = %q", r.Verdict)
	}
	if r.GradeGap != "—" {
		t.Errorf("grade_gap = %q", r.GradeGap)
	}
	if len(r.StudyPlan2Weeks) != 0 {
		t.Errorf("study_plan_2weeks не пустой: %v", r.StudyPlan2Weeks)
	}
	if len(r.ProgressVsPrevious) != 0 {
		t.Errorf("progress_vs_previous не пустой: %v", r.ProgressVsPrevious)
	}
	for _, c := range r.Criteria {
		if len(c.Evidence) != 0 || len(c.StudyPlan) != 0 {
			t.Errorf("критерий %q: evidence/study_plan не пустые", c.Name)
		}
		if c.GapToGrade != round2(3-c.Score) {
			t.Errorf("критерий %q: gap = %v, want 3-score", c.Name, c.GapToGrade)
		}
	}
}

// TestBuildProgress — delta = current − previous, same_stack.
func TestBuildProgress(t *testing.T) {
	sess := models.Session{Stack: models.StackGo}
	current := []CriterionScore{
		{Name: "A", Score: 4}, {Name: "B", Score: 2},
	}
	prev := []previousSession{
		{SessionID: 1, Date: "2026-01-01T00:00:00Z", Stack: "go", Grade: "middle",
			Overall: 3.0, Criteria: []CriterionScore{{Name: "A", Score: 3}, {Name: "C", Score: 5}}},
		{SessionID: 2, Date: "2026-01-02T00:00:00Z", Stack: "python", Grade: "middle",
			Overall: 2.5, Criteria: []CriterionScore{{Name: "B", Score: 1}}},
	}
	out := buildProgress(sess, current, prev)
	if len(out) != 2 {
		t.Fatalf("entries = %d, want 2", len(out))
	}
	if !out[0].SameStack {
		t.Error("entry 1: same_stack = false (go/go)")
	}
	if out[0].CriteriaDelta["A"] != 1 {
		t.Errorf("entry 1: delta A = %v, want 1", out[0].CriteriaDelta["A"])
	}
	if _, ok := out[0].CriteriaDelta["C"]; ok {
		t.Error("entry 1: delta по отсутствующему критерию C")
	}
	if out[1].SameStack {
		t.Error("entry 2: same_stack = true (python/go)")
	}
	if out[1].CriteriaDelta["B"] != 1 {
		t.Errorf("entry 2: delta B = %v, want 1", out[1].CriteriaDelta["B"])
	}
	if out[0].Overall != 3.0 || out[1].Overall != 2.5 {
		t.Errorf("overall: %v / %v", out[0].Overall, out[1].Overall)
	}
}
