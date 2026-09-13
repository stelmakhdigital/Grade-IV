// Package session — машина состояний сессии и движок тарификации (WP-3, ARCHITECTURE.md §4, SRS §7).
//
// Стадии: voice → livecode → [design] → report.
// Стадия System Design входит в программу грейдов Middle/Senior/Staff;
// у Junior программа: voice → livecode → report (SRS US-2).
//
// Статусы: active ↔ paused; финальные — finished (успешное завершение,
// включая выход на стадию report) и aborted (принудительное завершение:
// обрыв без возобновления дольше порога).
package session

import (
	"errors"
	"fmt"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// Ошибки машины состояний (маппинг на HTTP 409/404 — в httpapi).
var (
	ErrUnknownStage    = errors.New("неизвестная стадия")
	ErrUnknownStatus   = errors.New("неизвестный статус")
	ErrIllegalStage    = errors.New("недопустимый переход между стадиями")
	ErrIllegalStatus   = errors.New("недопустимый переход между статусами")
	ErrStageNotInScope = errors.New("стадия не входит в программу грейда")
)

// stageOrder — порядок прохождения стадий (report — последняя, терминальная).
var stageOrder = []models.Stage{
	models.StageVoice,
	models.StageLiveCode,
	models.StageDesign,
	models.StageReport,
}

// stagesForGrade — состав программы сессии по грейду.
func stagesForGrade(g models.Grade) []models.Stage {
	switch g {
	case models.GradeJunior:
		return []models.Stage{models.StageVoice, models.StageLiveCode, models.StageReport}
	default: // middle, senior, staff
		return stageOrder
	}
}

func stageIndex(s models.Stage) int {
	for i, st := range stageOrder {
		if st == s {
			return i
		}
	}
	return -1
}

// CanTransitionStage — допустим ли переход from → to для данного грейда.
// Перемещение вперёд разрешается только по программе грейда (без «перескоков»,
// но Junior пропускает design); возврат на предыдущую стадию разрешён,
// кроме report (терминальная).
func CanTransitionStage(grade models.Grade, from, to models.Stage) error {
	fromIdx, toIdx := stageIndex(from), stageIndex(to)
	if fromIdx < 0 || toIdx < 0 {
		return ErrUnknownStage
	}
	if toIdx == stageIndex(models.StageReport) {
		// report — только вперёд из последней рабочей стадии программы
		// (программа всегда завершается report, рабочая стадия — предпоследняя).
		prog := stagesForGrade(grade)
		if len(prog) < 2 || prog[len(prog)-2] != from {
			return fmt.Errorf("%w: %s → report", ErrIllegalStage, from)
		}
		return nil
	}
	if from == models.StageReport {
		return fmt.Errorf("%w: report → %s", ErrIllegalStage, to)
	}
	prog := stagesForGrade(grade)
	fromPos, toPos := -1, -1
	for i, st := range prog {
		switch st {
		case from:
			fromPos = i
		case to:
			toPos = i
		}
	}
	if fromPos < 0 || toPos < 0 {
		return fmt.Errorf("%w: %s", ErrStageNotInScope, to)
	}
	if toPos > fromPos+1 || toPos < fromPos-1 {
		return fmt.Errorf("%w: %s → %s", ErrIllegalStage, from, to)
	}
	return nil
}

// statusTransitions — допустимые переходы статусов.
var statusTransitions = map[models.Status]map[models.Status]bool{
	models.StatusActive:   {models.StatusPaused: true, models.StatusFinished: true, models.StatusAborted: true},
	models.StatusPaused:   {models.StatusActive: true, models.StatusFinished: true, models.StatusAborted: true},
	models.StatusFinished: {},
	models.StatusAborted:  {},
}

// CanTransitionStatus — допустим ли переход статусов.
func CanTransitionStatus(from, to models.Status) error {
	if to == from {
		return nil
	}
	if allowed, ok := statusTransitions[from]; !ok || !allowed[to] {
		return fmt.Errorf("%w: %s → %s", ErrIllegalStatus, from, to)
	}
	return nil
}

// IsTerminalStatus — статус финальный (finished/aborted).
func IsTerminalStatus(s models.Status) bool {
	return s == models.StatusFinished || s == models.StatusAborted
}
