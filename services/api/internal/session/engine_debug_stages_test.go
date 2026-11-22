package session

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// TestDebugStagesJump — отладка стадий (dev, ENABLE_DEBUG=1 → WithDebugStages):
// к dev-режиме можно перепрыгнуть сразу к доске, не проходя интервью с начала.
// Без флага правило «шаг ±1» обязано остаться как в проде.
func TestDebugStagesJump(t *testing.T) {
	conn, dialect, err := db.Open("sqlite://:memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx, conn, dialect); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	e := newTestEnvWithDB(t, conn, dialect)

	dev := models.Track{
		ID: "dev", Title: "Разработчик",
		Practice: &models.Practice{Runtime: models.StackGo},
		Grades:   []models.Grade{models.GradeMiddle},
	}
	pm := models.Track{
		ID: "pm", Title: "Продакт-менеджер",
		Grades: []models.Grade{models.GradeMiddle},
	}
	resolver := func(id string) (*models.Track, error) {
		switch id {
		case dev.ID:
			return &dev, nil
		case pm.ID:
			return &pm, nil
		}
		return nil, nil
	}

	// Дефолтный движок (prod): voice → design напрямую запрещён.
	e.engine.tracks = resolver
	strict, err := e.engine.Create(ctx, e.userID, models.GradeMiddle, models.StackGo, WithTrack(dev.ID))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := e.engine.Transition(strict.ID, models.StageDesign); !errors.Is(err, ErrIllegalStage) {
		t.Fatalf("без WithDebugStages голос→доска должны быть запрещены, получили: %v", err)
	}

	dbg := New(e.store, e.users, slog.New(slog.DiscardHandler),
		WithNow(func() time.Time { return *e.now }),
		WithTracks(resolver),
		WithDebugStages())

	devDbg, err := dbg.Create(ctx, e.userID, models.GradeMiddle, models.StackGo, WithTrack(dev.ID))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// report в отладке не открывается прыжком: он необратимо завершает сессию
	// (к нему по-прежнему нужно дойти по программе стадии).
	if _, err := dbg.Transition(devDbg.ID, models.StageReport); !errors.Is(err, ErrIllegalStage) {
		t.Fatalf("прыжок в report должен быть запрещён, получили: %v", err)
	}
	if _, err := dbg.Transition(devDbg.ID, models.StageDesign); err != nil {
		t.Fatalf("с флагом голос→доска обязаны проходить: %v", err)
	}
	snap, err := dbg.Snapshot(devDbg.ID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap.Stage != models.StageDesign {
		t.Fatalf("стадия после прыжка: %s", snap.Stage)
	}

	// Роль без практики: Live-Code в программе нет — прыжок туда запрещён и в отладке.
	pmDbg, err := dbg.Create(ctx, e.userID, models.GradeMiddle, models.StackGo, WithTrack(pm.ID))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := dbg.Transition(pmDbg.ID, models.StageLiveCode); !errors.Is(err, ErrStageNotInScope) {
		t.Fatalf("прыжок вне программы роли должен быть запрещён, получили: %v", err)
	}
}
