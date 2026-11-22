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

// stagesForTrack — программа стадий по треку и грейду (spec-18).
// track nil → по грейду (движок без хранилища треков, совместимость).
func stagesForTrack(track *models.Track, g models.Grade) []models.Stage {
	if track == nil {
		return stagesForGrade(g)
	}
	return track.Stages(g)
}

// stagePos — позиция стадии в программе (-1 — стадии в программе нет).
func stagePos(prog []models.Stage, s models.Stage) int {
	for i, st := range prog {
		if st == s {
			return i
		}
	}
	return -1
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

// CanTransitionStage — допустим ли переход from → to по программе стадий prog
// (spec-18). Программа передаётся списком, а не ролью: сессия живёт по
// программе, закреплённой при создании, а роль правят и во время интервью
// (issue #7). Перемещение вперёд разрешается только по программе (без
// «перескоков», но Junior backend-трека пропускает design); возврат на
// предыдущую стадию разрешён, кроме report (терминальная).
func CanTransitionStage(prog []models.Stage, from, to models.Stage) error {
	fromIdx, toIdx := stageIndex(from), stageIndex(to)
	if fromIdx < 0 || toIdx < 0 {
		return ErrUnknownStage
	}
	fromPos := stagePos(prog, from)
	if toIdx == stageIndex(models.StageReport) {
		// report — только вперёд из последней рабочей стадии программы
		// (программа всегда завершается report, рабочая стадия — предпоследняя).
		if fromPos < 0 {
			// Стадия выпала из программы: сессия создана до колонки sessions.stages
			// и роль поправили во время интервью (issue #7). Других переходов у
			// кандидата не остаётся — выпускаем в report, там сессия завершится.
			return nil
		}
		if len(prog) < 2 || prog[len(prog)-2] != from {
			return fmt.Errorf("%w: %s → report", ErrIllegalStage, from)
		}
		return nil
	}
	if from == models.StageReport {
		return fmt.Errorf("%w: report → %s", ErrIllegalStage, to)
	}
	toPos := stagePos(prog, to)
	if fromPos < 0 || toPos < 0 {
		return fmt.Errorf("%w: %s", ErrStageNotInScope, to)
	}
	if toPos > fromPos+1 || toPos < fromPos-1 {
		return fmt.Errorf("%w: %s → %s", ErrIllegalStage, from, to)
	}
	return nil
}

// CanJumpStageDebug — отладочный прыжок (dev, ENABLE_DEBUG=1): стадия обязана быть
// в программе сессии (придумать стадию нельзя), но шаг ±1 не требуется. report
// исключён: он необратимо завершает сессию, а для этого есть «Завершить интервью».
func CanJumpStageDebug(prog []models.Stage, to models.Stage) error {
	if to == models.StageReport {
		return fmt.Errorf("%w: %s (отчёт — завершением интервью)", ErrIllegalStage, to)
	}
	if stagePos(prog, to) >= 0 {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrStageNotInScope, to)
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
