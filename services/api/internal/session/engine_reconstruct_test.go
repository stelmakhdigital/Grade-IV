package session

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// TestTrackSurvivesReconstruct (issue #1): runtime, собранный из БД после
// «рестарта» движка, терял роль (rt.track == nil) — движок строил программу
// стадий по легаси-правилу грейда и не отдавал payload доски.
func TestTrackSurvivesReconstruct(t *testing.T) {
	conn, dialect, err := db.Open("sqlite://:memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx, conn, dialect); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	e := newTestEnvWithDB(t, conn, dialect, 3600)

	dev := models.Track{
		ID: "dev", Title: "Разработчик",
		Practice: &models.Practice{Runtime: models.StackGo},
		Grades:   []models.Grade{models.GradeJunior, models.GradeMiddle},
	}
	pm := models.Track{
		ID: "pm", Title: "Продакт-менеджер",
		Grades: []models.Grade{models.GradeJunior, models.GradeMiddle},
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
	e.engine.tracks = resolver

	// restart — новый движок поверх той же БД (после рестарта API рантаймов в памяти нет).
	restart := func() *Engine {
		return New(e.store, e.users, slog.New(slog.DiscardHandler),
			WithNow(func() time.Time { return *e.now }),
			WithTracks(resolver))
	}

	cases := []struct {
		name  string
		track models.Track
		grade models.Grade
		want  []models.Stage // программа роли: переходы по ней обязаны пройти
		deny  models.Stage   // стадии в программе роли нет
	}{
		// роль с практикой: junior получает design (spec-22), которого нет в
		// легаси-программе грейда.
		{name: "роль с практикой, junior", track: dev, grade: models.GradeJunior,
			want: []models.Stage{models.StageLiveCode, models.StageDesign}},
		// роль без практики: доска достижима из voice, Live-Code закрыт
		// (легаси-программа middle давала обратное).
		{name: "роль без практики, middle", track: pm, grade: models.GradeMiddle,
			want: []models.Stage{models.StageDesign}, deny: models.StageLiveCode},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, err := e.engine.Create(ctx, e.userID, tc.grade, models.StackGo,
				WithTrack(tc.track.ID))
			if err != nil {
				t.Fatalf("create: %v", err)
			}

			eng := restart()
			if _, err := eng.Attach(ctx, m.ID, e.userID, nil); err != nil {
				t.Fatalf("attach после рестарта: %v", err)
			}
			rt := eng.rt(m.ID)
			if rt == nil || rt.track == nil {
				t.Fatalf("роля потеряна при реконструкции: %+v", rt)
			}

			if tc.deny != "" {
				if _, err := eng.Transition(m.ID, tc.deny); err == nil {
					t.Fatalf("стадия %s доступна, хотя её в программе роли нет (%v)",
						tc.deny, tc.track.Stages(tc.grade))
				}
			}
			for _, to := range tc.want {
				if _, err := eng.Transition(m.ID, to); err != nil {
					t.Fatalf("переход к %s после рестарта: %v", to, err)
				}
			}
		})
	}
}

// TestTrackSurvivesReconstructFromStore — тот же трек восстанавливается и в
// пути без WS-соединения (reconstructFromStore: REST pause/resume/finish).
func TestTrackSurvivesReconstructFromStore(t *testing.T) {
	conn, dialect, err := db.Open("sqlite://:memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx, conn, dialect); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	e := newTestEnvWithDB(t, conn, dialect, 3600)
	sysadmin := models.Track{
		ID: "sysadmin", Title: "Системный администратор",
		Practice: &models.Practice{Runtime: models.StackShell},
		Grades:   []models.Grade{models.GradeJunior},
	}
	resolver := func(id string) (*models.Track, error) {
		if id == sysadmin.ID {
			return &sysadmin, nil
		}
		return nil, nil
	}
	e.engine.tracks = resolver
	m, err := e.engine.Create(ctx, e.userID, models.GradeJunior, models.StackGo,
		WithTrack(sysadmin.ID))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	eng := New(e.store, e.users, slog.New(slog.DiscardHandler),
		WithNow(func() time.Time { return *e.now }),
		WithTracks(resolver))
	if _, err := eng.Resume(m.ID); err != nil { // реконструкция из БД, без Attach
		t.Fatalf("resume после рестарта: %v", err)
	}
	rt := eng.rt(m.ID)
	if rt == nil || rt.track == nil {
		t.Fatalf("роля потеряна при реконструкции: %+v", rt)
	}
	if _, err := eng.Transition(m.ID, models.StageLiveCode); err != nil {
		t.Fatalf("voice→livecode: %v", err)
	}
	if _, err := eng.Transition(m.ID, models.StageDesign); err != nil {
		t.Fatalf("livecode→design (junior с практикой): %v", err)
	}
}
