package session

import (
	"testing"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

func TestCanTransitionStageForward(t *testing.T) {
	cases := []struct {
		grade models.Grade
		from  models.Stage
		to    models.Stage
		ok    bool
	}{
		{models.GradeMiddle, models.StageVoice, models.StageLiveCode, true},
		{models.GradeMiddle, models.StageLiveCode, models.StageDesign, true},
		{models.GradeMiddle, models.StageDesign, models.StageReport, true},
		{models.GradeSenior, models.StageVoice, models.StageLiveCode, true},
		{models.GradeStaff, models.StageDesign, models.StageReport, true},
		{models.GradeJunior, models.StageVoice, models.StageLiveCode, true},
		{models.GradeJunior, models.StageLiveCode, models.StageReport, true}, // Junior: без design
		// перескоки запрещены
		{models.GradeMiddle, models.StageVoice, models.StageDesign, false},
		{models.GradeMiddle, models.StageVoice, models.StageReport, false},
		// design не входит в программу Junior
		{models.GradeJunior, models.StageVoice, models.StageDesign, false},
		{models.GradeJunior, models.StageLiveCode, models.StageDesign, false},
		// неизвестные стадии
		{models.GradeMiddle, models.Stage(""), models.StageVoice, false},
	}
	for _, c := range cases {
		err := CanTransitionStage(c.grade, c.from, c.to)
		if c.ok && err != nil {
			t.Errorf("%s %s→%s: ожидалось допустимо, получено %v", c.grade, c.from, c.to, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s %s→%s: ожидалась ошибка", c.grade, c.from, c.to)
		}
	}
}

func TestCanTransitionStageBackward(t *testing.T) {
	if err := CanTransitionStage(models.GradeMiddle, models.StageLiveCode, models.StageVoice); err != nil {
		t.Errorf("возврат livecode→voice должен быть допустим: %v", err)
	}
	if err := CanTransitionStage(models.GradeMiddle, models.StageDesign, models.StageLiveCode); err != nil {
		t.Errorf("возврат design→livecode должен быть допустим: %v", err)
	}
	if err := CanTransitionStage(models.GradeMiddle, models.StageReport, models.StageVoice); err == nil {
		t.Error("из report назад переходить нельзя")
	}
}

func TestCanTransitionStatus(t *testing.T) {
	ok := [][2]models.Status{
		{models.StatusActive, models.StatusPaused},
		{models.StatusPaused, models.StatusActive},
		{models.StatusActive, models.StatusFinished},
		{models.StatusPaused, models.StatusFinished},
		{models.StatusActive, models.StatusAborted},
		{models.StatusPaused, models.StatusAborted},
		{models.StatusActive, models.StatusActive}, // idempotent
	}
	for _, p := range ok {
		if err := CanTransitionStatus(p[0], p[1]); err != nil {
			t.Errorf("%s→%s: %v", p[0], p[1], err)
		}
	}
	bad := [][2]models.Status{
		{models.StatusFinished, models.StatusActive},
		{models.StatusAborted, models.StatusPaused},
		{models.StatusFinished, models.StatusPaused},
	}
	for _, p := range bad {
		if err := CanTransitionStatus(p[0], p[1]); err == nil {
			t.Errorf("%s→%s: ожидалась ошибка", p[0], p[1])
		}
	}
}
