package interviewer

import (
	"fmt"
	"math"
	"strings"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// StageReport — оценка одной стадии по данным сессии (spec-24, инкремент 4).
//
// Зачем это нужно: раньше весь отчёт держался на трёх «если» (были проходы — 4,
// запуски без проходов — 3, запусков нет — 2). Число 4 при этом означало и
// «решил всё», и «решил одну задачу из трёх». Здесь каждая стадия считается
// отдельно и говорит, из чего взялось число.
//
// Это детерминированный слой: он не заменяет суждение LLM-оценщика, а остаётся
// под ним — цифру можно пересчитать по событиям сессии и оспорить.
type StageReport struct {
	Stage string  `json:"stage"`
	Title string  `json:"title"`
	Score float64 `json:"score"`
	// Basis — что именно посчитано (коротко, человеком).
	Basis    string   `json:"basis"`
	Evidence []string `json:"evidence,omitempty"`
	// Missing — чего не хватает по обязательным артефактам (стадия доски).
	Missing []string `json:"missing,omitempty"`
}

// boardBlocks — блоки схемы из события whiteboard_save.
func (m reportMaterial) boardBlocks() []string {
	raw, ok := m.Design["blocks"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, it := range raw {
		if s, ok := it.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

// boardLinks — число связей схемы.
func (m reportMaterial) boardLinks() int {
	if f, ok := m.Design["links"].(float64); ok {
		return int(f)
	}
	return 0
}

// lastRunsByTask — task_id → итог последней сдачи (passed).
func lastRunsByTask(mat reportMaterial) map[string]bool {
	last := map[string]bool{}
	for _, r := range assignedCodeRuns(mat) {
		id, _ := r["task_id"].(string)
		if id == "" {
			continue
		}
		p, _ := r["passed"].(bool)
		last[id] = p
	}
	return last
}

// liveCodeStageScore — Live-Code (и bash-практика: тот же механизм сдач).
//
// 1 + 4·доля решённых: ноль задач — 1, все задачи — 5. Если по части задач
// были сдачи, но они не прошли, добавляем полбалла: написанное и непрошедшее
// решение — не то же самое, что пустой редактор.
func liveCodeStageScore(mat reportMaterial) *StageReport {
	assigned := len(mat.LiveCodeTasks)
	if assigned == 0 {
		return nil
	}
	last := lastRunsByTask(mat)
	passed, attempted := 0, 0
	for _, t := range mat.LiveCodeTasks {
		p, ok := last[t.ID]
		if !ok {
			continue
		}
		attempted++
		if p {
			passed++
		}
	}
	score := 1 + 4*float64(passed)/float64(assigned)
	if attempted > passed {
		score += 0.5
	}
	score = clampScore(score)
	return &StageReport{
		Stage: string(models.StageLiveCode),
		Title: string("Live-Code"),
		Score: score,
		Basis: fmt.Sprintf("задач %d, сдано %d, пройдено %d", assigned, attempted, passed),
		Evidence: []string{
			fmt.Sprintf("прогонов по назначенным задачам: %d", len(assignedCodeRuns(mat))),
		},
	}
}

// designStageScore — доска: обязательные артефакты кейса (детерминированно) +
// связи. Если кейс обязательных артефактов не задаёт — считаем по факту схемы и
// честно об этом пишем: такая стадия не может дать выше 3,5.
func designStageScore(mat reportMaterial, track *models.Track, grade models.Grade) *StageReport {
	if len(mat.Design) == 0 {
		return nil
	}
	blocks := mat.boardBlocks()
	links := mat.boardLinks()
	title := "Доска"
	var c *models.BoardCase
	if track != nil {
		title = track.Board.Title
		c = track.Board.CaseForGrade(grade)
	}
	st := &StageReport{Stage: string(models.StageDesign), Title: title}
	st.Evidence = []string{fmt.Sprintf("на доске блоков: %d, связей: %d", len(blocks), links)}

	if c != nil && len(c.Required) > 0 {
		found, missing := c.Coverage(blocks)
		ratio := float64(len(found)) / float64(len(c.Required))
		score := 1 + 4*ratio
		if links > 0 {
			score += 0.5 // схема, в которой элементы соединены, — не набор квадратиков
		}
		st.Score = clampScore(score)
		st.Basis = fmt.Sprintf("обязательных артефактов кейса: %d из %d", len(found), len(c.Required))
		st.Missing = missing
		return st
	}
	score := 2.0
	if len(blocks) > 0 {
		score = 3
		if links > 0 {
			score = 3.5
		}
	}
	st.Score = score
	st.Basis = "кейс без обязательных артефактов — оценка по наличию схемы"
	return st
}

// speechStageScore — устная часть: сколько кандидат говорил и насколько
// развёрнуто. Балл за коммуникацию «по факту речи» честнее, чем «была речь — 3»:
// односложные «да» на 30 репликах и развёрнутые объяснения — разные интервью.
func speechStageScore(mat reportMaterial) *StageReport {
	utterances, chars := 0, 0
	short := 0
	for _, line := range mat.Transcript {
		if !strings.HasPrefix(line, "кандидат:") {
			continue
		}
		text := strings.TrimSpace(strings.TrimPrefix(line, "кандидат:"))
		if text == "" {
			continue
		}
		utterances++
		chars += len([]rune(text))
		if len([]rune(text)) < 40 {
			short++
		}
	}
	if utterances == 0 {
		return &StageReport{
			Stage: string(models.StageVoice),
			Title: "Устная часть",
			Score: 1,
			Basis: "реплик кандидата не было",
		}
	}
	avg := float64(chars) / float64(utterances)
	score := 2 + math.Min(2, float64(utterances)/6)
	if avg >= 120 {
		score += 1 // ответы развёрнутые, а не «да/нет»
	}
	if avg >= 300 {
		score += 0.5
	}
	return &StageReport{
		Stage: string(models.StageVoice),
		Title: "Устная часть",
		Score: clampScore(score),
		Basis: fmt.Sprintf("реплик %d, средняя длина %.0f знаков", utterances, avg),
		Evidence: []string{
			fmt.Sprintf("коротких реплик (<40 знаков): %d", short),
		},
	}
}

// stageReports — детерминированные оценки по стадиям, которые реально были.
// Стадии, по которым данных нет, в список не попадают: «не было доски» — это не
// ноль балла, а отсутствие измерения.
func stageReports(mat reportMaterial, track *models.Track) []StageReport {
	var out []StageReport
	if s := speechStageScore(mat); s != nil {
		out = append(out, *s)
	}
	if s := liveCodeStageScore(mat); s != nil {
		out = append(out, *s)
	}
	if s := designStageScore(mat, track, models.Grade(mat.Grade)); s != nil {
		out = append(out, *s)
	}
	return out
}

// stageSummary — та же картина одной строкой, в промпт LLM-оценщика: пусть
// рассуждает над фактами, а не пересчитывает их на глаз.
func stageSummary(stages []StageReport) string {
	if len(stages) == 0 {
		return "стадий с данными нет"
	}
	parts := make([]string, 0, len(stages))
	for _, s := range stages {
		line := fmt.Sprintf("%s: %.1f из 5 (%s)", s.Title, s.Score, s.Basis)
		if len(s.Missing) > 0 {
			line += "; нет на доске: " + strings.Join(s.Missing, ", ")
		}
		parts = append(parts, line)
	}
	return strings.Join(parts, "; ")
}

func clampScore(v float64) float64 {
	if v < 1 {
		return 1
	}
	if v > 5 {
		return 5
	}
	return round2(v)
}

// stageByStage — оценка стадии по её ключу (nil — стадии не было).
func stageByStage(stages []StageReport, stage string) *StageReport {
	for i := range stages {
		if stages[i].Stage == stage {
			return &stages[i]
		}
	}
	return nil
}

// stageStrengths / stageWeaknesses — авто-отчёт говорит тем, что измерено:
// сильные стороны — стадии от 4, слабые — до 2,5 и отсутствующие артефакты.
func stageStrengths(stages []StageReport) []string {
	out := []string{}
	for _, s := range stages {
		if s.Score >= 4 {
			out = append(out, fmt.Sprintf("%s: %.1f из 5 (%s)", s.Title, s.Score, s.Basis))
		}
	}
	return out
}

func stageWeaknesses(stages []StageReport) []string {
	out := []string{}
	for _, s := range stages {
		if s.Score <= 2.5 {
			out = append(out, fmt.Sprintf("%s: %.1f из 5 (%s)", s.Title, s.Score, s.Basis))
		}
		for _, m := range s.Missing {
			out = append(out, fmt.Sprintf("%s: на доске нет обязательного «%s»", s.Title, m))
		}
	}
	return out
}
