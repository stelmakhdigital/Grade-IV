package db

import (
	"context"
	"strings"
	"testing"
)

var wantedTables = []string{
	"users", "sessions", "session_events", "submissions",
	"whiteboards", "reports", "minutes_ledger",
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

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
