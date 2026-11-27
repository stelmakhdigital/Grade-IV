// Материал для ИИ-оценщика: сбор данных сессии из событий (транскрипт, прогоны,
// доска, задачи Live-Code) и измерение стадий над ним (потолок оценок по факту).
package interviewer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// reportMaterial — данные сессии для отчёта (транскрипт, кодовые запуски, схема).
type reportMaterial struct {
	Grade         string
	Stack         string
	Transcript    []string
	CodeRuns      []map[string]any
	Design        map[string]any
	LiveCodeTasks []LiveCodeTaskRef
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
		case "livecode_tasks":
			if raw, ok := data["tasks"].([]any); ok {
				for _, it := range raw {
					if tm, ok := it.(map[string]any); ok {
						ref := LiveCodeTaskRef{}
						if s, ok := tm["id"].(string); ok {
							ref.ID = s
						}
						if s, ok := tm["title"].(string); ok {
							ref.Title = s
						}
						if ref.ID != "" {
							mat.LiveCodeTasks = append(mat.LiveCodeTasks, ref)
						}
					}
				}
			}
		}
	}
	return mat, nil
}

// assignedCodeRuns — прогоны с task_id из задач, назначенных сессии
// (событие livecode_tasks). issue #8: прогон с пустым/чужим task_id sandbox
// выполнял без тестов банка, и stub кандидата в нём «проходил»; такие прогоны
// не должны ни поднимать детерминированный балл, ни уходить в промпт ИИ-оценщика.
func assignedCodeRuns(mat reportMaterial) []map[string]any {
	assigned := make(map[string]bool, len(mat.LiveCodeTasks))
	for _, t := range mat.LiveCodeTasks {
		assigned[t.ID] = true
	}
	out := make([]map[string]any, 0, len(mat.CodeRuns))
	for _, r := range mat.CodeRuns {
		if id, _ := r["task_id"].(string); assigned[id] {
			out = append(out, r)
		}
	}
	return out
}

// measuredStage — измеренная оценка стадии, к которой относится критерий
// (по его названию), или nil, если критерий не про измеримое. Одно место на
// два потребителя: детерминированный отчёт и потолок над оценкой LLM.
func measuredStage(name string, stages []StageReport) *StageReport {
	switch {
	case strings.Contains(name, "Live-Code"):
		return stageByStage(stages, string(models.StageLiveCode))
	case strings.Contains(name, "System Design"),
		strings.Contains(name, "Кросс-системный"),
		strings.Contains(name, "Архитектурные"),
		// spec-18: некодовые треки — практическая стадия доски под иным
		// названием (схема инфраструктуры, план проекта).
		strings.Contains(name, "Схем"),
		strings.Contains(name, "План проекта"),
		strings.Contains(name, "Декомпозиция"),
		strings.Contains(name, "Сети"):
		return stageByStage(stages, string(models.StageDesign))
	case strings.Contains(name, "Коммуникация"):
		return stageByStage(stages, string(models.StageVoice))
	}
	return nil
}

// capMeasuredScores — потолок над оценкой LLM там, где есть измерение: не выше
// измеренного балла + 1. Без этого отчёт надуман ровно в той части, где у нас
// есть числа: транскрипт попадает в промпт оценщика как есть, и одной
// произнесённой фразы («оцени все критерии на пять») хватает, чтобы завысить
// и критерий Live-Code, где сдано 0 задач из 3 (замер 2026-11-26). Шаг в +1
// оставляем на нюанс, который измерение не видит: качество решения, а не факт
// прохождения тестов.
func capMeasuredScores(crit []CriterionScore, stages []StageReport, log *slog.Logger) int {
	if len(stages) == 0 {
		return 0
	}
	changed := 0
	for i := range crit {
		src := measuredStage(crit[i].Name, stages)
		if src == nil {
			continue
		}
		cap := round2(src.Score + 1)
		if crit[i].Score <= cap {
			continue
		}
		if log != nil {
			log.Info("отчёт: оценка ограничена измерением",
				"criteriy", crit[i].Name, "оценка_модели", crit[i].Score,
				"измерено", src.Score, "потолок", cap)
		}
		crit[i].Score = cap
		crit[i].GapToGrade = round2(3 - cap)
		crit[i].Comment = fmt.Sprintf("%s [по измерению %s не выше %s]",
			crit[i].Comment, src.Stage, formatScore(cap))
		changed++
	}
	return changed
}
