package session

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// devTrack — роль с практикой (программа: voice → livecode → design → report).
func devTrack() models.Track {
	return models.Track{
		ID:       "dev",
		Title:    "Разработчик",
		Practice: &models.Practice{Runtime: models.StackGo},
		Grades:   []models.Grade{models.GradeJunior, models.GradeMiddle},
	}
}

// TestStageProgramFrozenAtCreate (issue #7): роль правят во время идущей сессии
// («Практика в редакторе» снята). Сессия обязана жить по программе стадий,
// какой она была на момент старта: кандидат на Live-Code должен сохранить
// livecode → design → report. До фикса программа резолвилась по живой роли, и
// после рестарта API livecode выпадает из программы — все переходы закрыты,
// выход только «Завершить» (доска недоступна).
func TestStageProgramFrozenAtCreate(t *testing.T) {
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

	cur := devTrack()
	resolver := func(id string) (*models.Track, error) {
		if id != cur.ID {
			return nil, nil
		}
		cp := cur
		return &cp, nil
	}
	e.engine.tracks = resolver

	restart := func() *Engine {
		return New(e.store, e.users, slog.New(slog.DiscardHandler),
			WithNow(func() time.Time { return *e.now }),
			WithTracks(resolver))
	}

	m, err := e.engine.Create(ctx, e.userID, models.GradeMiddle, models.StackGo,
		WithTrack(cur.ID))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := e.engine.Transition(m.ID, models.StageLiveCode); err != nil {
		t.Fatalf("voice→livecode: %v", err)
	}

	// Админ снимает практику и сохраняет роль.
	cur.Practice = nil

	// Рестарт API: рантаймов в памяти нет, кандидат переподключается.
	eng := restart()
	if _, err := eng.Attach(ctx, m.ID, e.userID, nil); err != nil {
		t.Fatalf("attach после рестарта: %v", err)
	}
	for _, to := range []models.Stage{models.StageDesign, models.StageReport} {
		if _, err := eng.Transition(m.ID, to); err != nil {
			t.Fatalf("переход к %s после смены роли: %v", to, err)
		}
	}
	snap, err := eng.Snapshot(m.ID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap.Status != models.StatusFinished {
		t.Fatalf("сессия не завершена на report: %s", snap.Status)
	}

	// Новая сессия на той же роли живёт по НОВОЙ программе: практики нет.
	eng2 := restart()
	fresh, err := eng2.Create(ctx, e.userID, models.GradeMiddle, models.StackGo,
		WithTrack(cur.ID))
	if err != nil {
		t.Fatalf("create после смены роли: %v", err)
	}
	if _, err := eng2.Transition(fresh.ID, models.StageLiveCode); err == nil {
		t.Fatal("livecode доступен для роли без практики — программа не обновилась у новой сессии")
	}
}
