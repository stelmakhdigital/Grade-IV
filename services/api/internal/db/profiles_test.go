package db

import (
	"context"
	"database/sql"
	"testing"
)

func newTestDB(t *testing.T) *profileTestEnv {
	t.Helper()
	conn, dialect, err := Open("sqlite://:memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := Migrate(context.Background(), conn, dialect); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &profileTestEnv{conn: conn, d: dialect, store: NewProfileStore(conn, dialect)}
}

type profileTestEnv struct {
	conn  *sql.DB
	d     Dialect
	store *ProfileStore
}

// TestEnsurePresets — сид 10 пресетов, идемпотентность (второй вызов — no-op).
func TestEnsurePresets(t *testing.T) {
	env := newTestDB(t)
	ctx := context.Background()

	if err := env.store.EnsurePresets(ctx, PresetProfiles()); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	list, err := env.store.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 10 {
		t.Fatalf("пресетов = %d, want 10", len(list))
	}
	for _, p := range list {
		if !p.IsPreset {
			t.Errorf("пресет %q: is_preset = false", p.Name)
		}
		if !validProfileValue(p.Tone, "tone") || !validProfileValue(p.Difficulty, "difficulty") {
			t.Errorf("пресет %q: недопустимый tone/difficulty (%s/%s)", p.Name, p.Tone, p.Difficulty)
		}
	}
	// Идемпотентность: повторный вызов ничего не добавляет.
	if err := env.store.EnsurePresets(ctx, PresetProfiles()); err != nil {
		t.Fatalf("ensure 2nd: %v", err)
	}
	list, err = env.store.List(ctx)
	if err != nil {
		t.Fatalf("list 2nd: %v", err)
	}
	if len(list) != 10 {
		t.Fatalf("после 2го ensure: пресетов = %d, want 10", len(list))
	}
}

// TestProfileCRUD — Create (с валидацией), Get, ErrProfileNotFound, List.
func TestProfileCRUD(t *testing.T) {
	env := newTestDB(t)
	ctx := context.Background()
	_ = env.store.EnsurePresets(ctx, PresetProfiles())

	p, err := env.store.Create(ctx, Profile{Name: "Свой", Tone: TonePlayful, Difficulty: DifficultyMinus, Description: "тест"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.ID == 0 || p.Name != "Свой" || p.IsPreset {
		t.Fatalf("созданный профиль: %+v", p)
	}
	got, err := env.store.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Tone != TonePlayful || got.Difficulty != DifficultyMinus || got.Description != "тест" {
		t.Fatalf("get: %+v", got)
	}
	if _, err := env.store.Get(ctx, 999999); err != ErrProfileNotFound {
		t.Fatalf("get(999999): err = %v, want ErrProfileNotFound", err)
	}
	for _, bad := range []Profile{
		{Name: "X", Tone: "aggressive", Difficulty: DifficultyStandard},
		{Name: "X", Tone: ToneStrict, Difficulty: "nightmare"},
		{Name: "", Tone: ToneStrict, Difficulty: DifficultyStandard},
	} {
		if _, err := env.store.Create(ctx, bad); err == nil {
			t.Errorf("create(%+v): ожидалась ошибка валидации", bad)
		}
	}
	list, err := env.store.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 11 {
		t.Fatalf("list: профилей = %d, want 11", len(list))
	}
	// Пресеты первыми в списке.
	if !list[0].IsPreset {
		t.Errorf("первый в списке должен быть пресетом: %+v", list[0])
	}
}
