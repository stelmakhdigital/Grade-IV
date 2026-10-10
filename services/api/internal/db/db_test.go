package db

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

var wantedTables = []string{
	"users", "sessions", "session_events", "submissions",
	"whiteboards", "reports", "minutes_ledger", "interviewer_profiles",
}

func TestMigrateSqlite(t *testing.T) {
	conn, dialect, err := Open("sqlite://:memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	if dialect != DialectSQLite {
		t.Fatalf("dialect = %q, want sqlite", dialect)
	}
	if err := Migrate(context.Background(), conn, dialect); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	rows, err := conn.Query(`SELECT name FROM sqlite_master WHERE type = 'table'`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan: %v", err)
		}
		names = append(names, n)
	}
	for _, want := range wantedTables {
		if !contains(names, want) {
			t.Errorf("таблица %q не создана (есть: %s)", want, strings.Join(names, ", "))
		}
	}
}

func TestUserStore(t *testing.T) {
	conn, dialect, err := Open("sqlite://:memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	if err := Migrate(context.Background(), conn, dialect); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := NewUserStore(conn, dialect)
	ctx := context.Background()

	u, err := store.CreateUser(ctx, "A@B.ru", "hash-1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if u.Email != "a@b.ru" {
		t.Fatalf("email = %q, want a@b.ru (нормализация регистра)", u.Email)
	}
	if _, err := store.CreateUser(ctx, "a@b.ru", "hash-2"); err == nil {
		t.Fatal("create duplicate: ожидалась ошибка ErrEmailExists")
	}
	got, err := store.GetUserByEmail(ctx, "A@B.ru")
	if err != nil {
		t.Fatalf("get by email: %v", err)
	}
	if got.ID != u.ID {
		t.Fatalf("id = %d, want %d", got.ID, u.ID)
	}
	if _, err := store.GetUserByEmail(ctx, "nobody@nowhere.ru"); err != ErrUserNotFound {
		t.Fatalf("err = %v, want ErrUserNotFound", err)
	}
	if err := store.GrantMinutes(ctx, u.ID, nil, 3600, "grant_free"); err != nil {
		t.Fatalf("grant: %v", err)
	}
	remaining, err := store.MinutesRemaining(ctx, u.ID)
	if err != nil {
		t.Fatalf("remaining: %v", err)
	}
	if remaining != 3600 {
		t.Fatalf("remaining = %d, want 3600", remaining)
	}
}

func TestSessionCreateDefaultProfileNoFKViolation(t *testing.T) {
	conn, dialect, err := Open("sqlite://:memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	if err := Migrate(context.Background(), conn, dialect); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()
	users := NewUserStore(conn, dialect)
	u, err := users.CreateUser(ctx, "fk@test.dev", "hash-fk")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	sessions := NewSessionStore(conn, dialect)

	// profile_id=0 / template_id=0 — дефолты, должны стать NULL (без FK-нарушения).
	m, err := sessions.Create(ctx, models.Session{
		UserID: u.ID, Grade: models.Grade("middle"), Stack: models.Stack("go"),
		Stage: models.Stage("voice"), Status: models.Status("active"), StartedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("create с дефолтным profile_id (0): %v", err)
	}
	if m.ProfileID != 0 || m.TemplateID != 0 {
		t.Fatalf("дефолты: ProfileID=%d TemplateID=%d, want 0/0 (NULL)", m.ProfileID, m.TemplateID)
	}

	// Реальный профиль — должен сохраниться.
	p, err := NewProfileStore(conn, dialect).Create(ctx, Profile{
		Name: "Тест", Tone: "strict", Difficulty: "plus", IsPreset: false, CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	m2, err := sessions.Create(ctx, models.Session{
		UserID: u.ID, Grade: models.Grade("middle"), Stack: models.Stack("go"),
		Stage: models.Stage("voice"), Status: models.Status("active"),
		StartedAt: time.Now(), ProfileID: p.ID,
	})
	if err != nil {
		t.Fatalf("create с profile: %v", err)
	}
	if m2.ProfileID != p.ID {
		t.Fatalf("ProfileID = %d, want %d", m2.ProfileID, p.ID)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
