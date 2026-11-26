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
	r, ok := parseReportJSON(content, criteriaForGrade(models.GradeMiddle), 0)
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
	r, ok := parseReportJSON(content, criteriaForGrade(models.GradeMiddle), 0)
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
	r := heuristicReport(sess, mat, nil)
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

// TestHeuristicReportTaskIDGate — issue #8: прогон с пустым или чужим task_id
// (sandbox не подставляет тесты банка, и stub кандидата «проходит») не должен
// поднимать балл Live-Code.
//
// spec-24 меняет сами числа: было «есть проходы — 4» независимо от того, сколько
// задач решено из трёх; теперь 1 + 4·доля решённых. Смысл гейта не изменился —
// чужой прогон не даёт ни доли, ни балла.
func TestHeuristicReportTaskIDGate(t *testing.T) {
	sess := models.Session{Grade: models.GradeMiddle, Stack: models.StackGo}
	liveCodeScore := func(mat reportMaterial) float64 {
		for _, c := range heuristicReport(sess, mat, nil).Criteria {
			if strings.Contains(c.Name, "Live-Code") {
				return c.Score
			}
		}
		t.Fatal("нет критерия Live-Code")
		return 0
	}
	one := []LiveCodeTaskRef{{ID: "go-a", Title: "А"}}
	three := []LiveCodeTaskRef{{ID: "go-a"}, {ID: "go-b"}, {ID: "go-c"}}

	cases := []struct {
		name     string
		assigned []LiveCodeTaskRef
		runs     []map[string]any
		want     float64
	}{
		{"passed с пустым task_id", one, []map[string]any{{"task_id": "", "passed": true}}, 1},
		{"passed с чужим task_id", one, []map[string]any{{"task_id": "go-z", "passed": true}}, 1},
		{"passed по назначенной задаче (1 из 1)", one, []map[string]any{{"task_id": "go-a", "passed": true}}, 5},
		// 1 из 3 с сдачами по остальным: 1 + 4·1/3 + 0.5 (пытался везде)
		{"passed 1 из 3, сдавался по всем", three, []map[string]any{
			{"task_id": "go-a", "passed": true},
			{"task_id": "go-b", "passed": false},
			{"task_id": "go-c", "passed": false},
		}, 2.83},
		// 1 из 3 и больше ни одной сдачи: полбалла за попытки нет
		{"passed 1 из 3, других сдач нет", three, []map[string]any{
			{"task_id": "go-a", "passed": true},
		}, 2.33},
		{"сдач нет вовсе", one, nil, 1},
	}
	for _, tc := range cases {
		got := liveCodeScore(reportMaterial{LiveCodeTasks: tc.assigned, CodeRuns: tc.runs})
		if got != tc.want {
			t.Errorf("%s: score = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestHeuristicReportStageScores — инкремент 4: критерии берутся из стадий, а
// «нет данных» не превращается в ноль.
func TestHeuristicReportStageScores(t *testing.T) {
	sess := models.Session{Grade: models.GradeSenior, Stack: models.StackGo}
	mat := reportMaterial{
		Grade:         "senior",
		LiveCodeTasks: []LiveCodeTaskRef{{ID: "go-a"}, {ID: "go-b"}},
		CodeRuns:      []map[string]any{{"task_id": "go-a", "passed": true}, {"task_id": "go-b", "passed": true}},
		Design:        map[string]any{"blocks": []any{"БД", "Кэш"}, "links": float64(3)},
		Transcript:    []string{"кандидат: " + strings.Repeat("объяснение архитектуры ", 20), "интервьюер: вопрос"},
	}
	r := heuristicReport(sess, mat, nil)
	byStage := map[string]StageReport{}
	for _, st := range r.Stages {
		byStage[st.Stage] = st
	}
	if len(r.Stages) != 3 {
		t.Fatalf("стадий %d, want 3 (устная, livecode, доска): %+v", len(r.Stages), r.Stages)
	}
	if byStage["livecode"].Score != 5 {
		t.Errorf("livecode = %v, want 5 (обе задачи решены)", byStage["livecode"].Score)
	}
	if byStage["design"].Score < 3 {
		t.Errorf("design = %v, want >= 3 (схема с блоками и связями)", byStage["design"].Score)
	}
	if byStage["voice"].Score <= 2 {
		t.Errorf("voice = %v, want > 2 (развёрнутые ответы)", byStage["voice"].Score)
	}
	for _, c := range r.Criteria {
		switch {
		case strings.Contains(c.Name, "Live-Code"):
			if c.Score != 5 {
				t.Errorf("критерий %q = %v, want 5", c.Name, c.Score)
			}
		case strings.Contains(c.Name, "System Design"):
			if c.Score != byStage["design"].Score {
				t.Errorf("критерий %q = %v, want %v", c.Name, c.Score, byStage["design"].Score)
			}
			if !strings.Contains(c.Comment, "доска") && !strings.Contains(c.Comment, "Доска") && c.Comment == "" {
				t.Errorf("критерий %q: в комментарии нет основания оценки: %q", c.Name, c.Comment)
			}
		}
	}
}

// TestHeuristicReportNoBoard — стадии, по которым данных нет, не оцениваются и
// не тянут отчёт вниз.
func TestHeuristicReportNoBoard(t *testing.T) {
	sess := models.Session{Grade: models.GradeMiddle, Stack: models.StackGo}
	r := heuristicReport(sess, reportMaterial{Grade: "middle"}, nil)
	for _, st := range r.Stages {
		if st.Stage == "design" {
			t.Fatalf("оценка доски появилась без данных доски: %+v", st)
		}
	}
	for _, c := range r.Criteria {
		if strings.Contains(c.Name, "System Design") && c.Score != 3 {
			t.Errorf("%q = %v, want 3 (нет данных — средняя, а не ноль)", c.Name, c.Score)
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

// TestCapMeasuredScores — оценка LLM по измеримому критерию не улетает выше
// измерения. Проверено на живом стенде: транскрипт идёт в промпт оценщика как
// есть, и одной фразы кандидата хватало, чтобы Live-Code с 0 решёнными задачами
// получил 5 (замер 2026-11-26).
func TestCapMeasuredScores(t *testing.T) {
	stages := []StageReport{
		{Stage: "livecode", Title: "Live-Code", Score: 1, Basis: "0 из 3 задач"},
		{Stage: "voice", Title: "Голосовая стадия", Score: 2, Basis: "2 реплики"},
	}
	crit := criteriaForGrade(models.GradeMiddle)
	for i := range crit {
		crit[i].Score = 5
		crit[i].GapToGrade = -2
	}
	if n := capMeasuredScores(crit, stages, nil); n == 0 {
		t.Fatal("потолок не сработал ни на одном критерии")
	}
	var cappedLiveCode, cappedVoice int
	for _, c := range crit {
		switch {
		case strings.Contains(c.Name, "Live-Code"):
			cappedLiveCode++
			if c.Score > 2 {
				t.Errorf("%v = %v, хочу не больше 2 (измерено 1 + 1)", c.Name, c.Score)
			}
			if !strings.Contains(c.Comment, "не выше") {
				t.Errorf("в комментарии не видно ограничения: %q", c.Comment)
			}
			if c.GapToGrade != 1 {
				t.Errorf("gap_to_grade = %v, хочу 1 (3-2)", c.GapToGrade)
			}
		case strings.Contains(c.Name, "Коммуникация"):
			cappedVoice++
			if c.Score > 3 {
				t.Errorf("%v = %v, хочу не больше 3 (измерено 2 + 1)", c.Name, c.Score)
			}
		case c.Score != 5:
			// Неизмеримый критерий (фундамент, leadership) трогать нельзя:
			// там у измерения нет данных, и решение остаётся за оценщиком.
			t.Errorf("%v = %v, неизмеримый критерий задет", c.Name, c.Score)
		}
	}
	if cappedLiveCode == 0 || cappedVoice == 0 {
		t.Fatalf("критерии не сопоставлены стадиям: livecode=%d voice=%d", cappedLiveCode, cappedVoice)
	}
	// Идемпотентность: повторный проход ничего не меняет.
	if n := capMeasuredScores(crit, stages, nil); n != 0 {
		t.Errorf("повторный вызов изменил %d критериев", n)
	}
}

// Нет измерений (LLM не вернул stages, пустая сессия) — потолок не применяется,
// иначе он срезал бы отчёт до 1 по всем критериям.
func TestCapMeasuredScoresWithoutMeasurements(t *testing.T) {
	crit := criteriaForGrade(models.GradeMiddle)
	for i := range crit {
		crit[i].Score = 5
	}
	if n := capMeasuredScores(crit, nil, nil); n != 0 {
		t.Fatalf("без измерений срезано %d критериев", n)
	}
	for _, c := range crit {
		if c.Score != 5 {
			t.Errorf("%v срезан до %v без измерений", c.Name, c.Score)
		}
	}
}
