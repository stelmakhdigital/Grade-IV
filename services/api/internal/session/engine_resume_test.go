package session

import (
	"context"
	"testing"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// multiTrack — роль, которая умеет и go (дефолт практики), и python.
func multiTrack() models.Track {
	t := devTrack()
	t.PracticeStacks = []models.Stack{models.StackGo, models.StackPython}
	return t
}

// TestResumeStackWinsOverRoleDefault: резюме на python, роль по умолчанию go —
// кодовая стадия обязана быть python. Пока стек брался только из
// practice.runtime, Python-кандидат решал go-задачи и получал по ним оценку
// (замер 2026-11-26: резюме FastAPI/asyncio, сессия backend/go).
// Должность и языки из резюме должны сохраняться и читаться обратно: по ним
// заголовок интервью и директива «спрашивать по всем заявленным».
func TestResumeStackWinsOverRoleDefault(t *testing.T) {
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
	track := multiTrack()
	e.engine.tracks = func(id string) (*models.Track, error) {
		if id != track.ID {
			return nil, nil
		}
		cp := track
		return &cp, nil
	}

	m, err := e.engine.Create(ctx, e.userID, models.GradeMiddle, models.StackGo,
		WithTrack(track.ID),
		WithResumeProfile("Python-разработчик", []models.Stack{models.StackPython, models.StackGo}))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if m.Stack != models.StackPython {
		t.Fatalf("кодовая стадия %q, хочу python", m.Stack)
	}
	if m.ResumeTitle != "Python-разработчик" {
		t.Errorf("заголовок по резюме = %q", m.ResumeTitle)
	}

	// Круг через БД: колонки resume_title/resume_stacks реально пишутся.
	back, err := e.store.Get(ctx, m.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if back.ResumeTitle != "Python-разработчик" {
		t.Errorf("после чтения заголовок = %q", back.ResumeTitle)
	}
	want := []models.Stack{models.StackPython, models.StackGo}
	if len(back.ResumeStacks) != len(want) {
		t.Fatalf("языки после чтения = %v, хочу %v", back.ResumeStacks, want)
	}
	for i := range want {
		if back.ResumeStacks[i] != want[i] {
			t.Fatalf("языки после чтения = %v, хочу %v", back.ResumeStacks, want)
		}
	}
}

// TestResumeStackUnsupportedKeepsRoleRuntime: роль python не умеет — кодовая
// стадия остаётся на рантайме роли. Иначе уйдем в банк задач и рантайм, которых
// для этой роли нет.
func TestResumeStackUnsupportedKeepsRoleRuntime(t *testing.T) {
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
	track := devTrack() // только go
	e.engine.tracks = func(id string) (*models.Track, error) {
		if id != track.ID {
			return nil, nil
		}
		cp := track
		return &cp, nil
	}

	m, err := e.engine.Create(ctx, e.userID, models.GradeMiddle, models.StackGo,
		WithTrack(track.ID),
		WithResumeProfile("Python-разработчик", []models.Stack{models.StackPython}))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if m.Stack != models.StackGo {
		t.Fatalf("кодовая стадия %q, хочу go (роль python не умеет)", m.Stack)
	}
}

func TestPickResumeStack(t *testing.T) {
	multi := multiTrack()
	solo := devTrack()
	for _, tc := range []struct {
		name   string
		resume []models.Stack
		track  *models.Track
		want   models.Stack
	}{
		{"первый поддерживаемый", []models.Stack{models.StackPython, models.StackGo}, &multi, models.StackPython},
		{"порядок задаёт резюме", []models.Stack{models.StackGo, models.StackPython}, &multi, models.StackGo},
		{"дефолт роли тоже проходит", []models.Stack{models.StackGo}, &solo, models.StackGo},
		{"неизвестный язык", []models.Stack{models.Stack("rust")}, &multi, ""},
		{"роль без практики", []models.Stack{models.StackPython}, &models.Track{ID: "pm"}, ""},
		{"резюме без языков", nil, &multi, ""},
	} {
		if got := pickResumeStack(tc.resume, tc.track); got != tc.want {
			t.Errorf("%s: pickResumeStack = %q, хочу %q", tc.name, got, tc.want)
		}
	}
}
