package db

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

var wantedTables = []string{
	"users", "sessions", "session_events", "submissions",
	"whiteboards", "reports", "interviewer_profiles",
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

// TestMigrateShareIndex — миграция идемпотентна (её гоняют на каждом старте
// api), и поиск по публичному токену отчёта ходит по индексу, а не по таблице.
func TestMigrateShareIndex(t *testing.T) {
	conn, dialect, err := Open("sqlite://:memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	ctx := context.Background()
	if err := Migrate(ctx, conn, dialect); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Migrate вызывается на каждом старте api — второй прогон обязан быть
	// безошибочным (в том числе по CREATE INDEX IF NOT EXISTS).
	if err := Migrate(ctx, conn, dialect); err != nil {
		t.Fatalf("migrate повторно: %v", err)
	}
	// План проверяем на наполненной таблице: на пустой планировщик вправе
	// выбрать SCAN, и тест ничего не проверял бы.
	users, sessions := NewUserStore(conn, dialect), NewSessionStore(conn, dialect)
	u, err := users.CreateUser(ctx, "share-index@example.com", "hash")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	for i := 0; i < 20; i++ {
		if _, err := sessions.Create(ctx, models.Session{
			UserID: u.ID, Grade: models.GradeMiddle, Stack: "go",
			Stage: models.StageVoice, Status: models.StatusActive,
			DurationLimitS: 1800, StartedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("session: %v", err)
		}
	}
	// Токен ищется при каждом открытии публичной ссылки. Запрос обязан нести
	// и условие `<> ''`: индекс частичный (у большинства сессий токена нет), а
	// планировщик сам не выводит из `= 'tok'`, что это непустая строка.
	var selectid, ord, from int
	var detail string
	if err := conn.QueryRow(
		`EXPLAIN QUERY PLAN SELECT id FROM sessions WHERE share_token = 'tok' AND share_token <> ''`).
		Scan(&selectid, &ord, &from, &detail); err != nil {
		t.Fatalf("explain: %v", err)
	}
	if !strings.Contains(detail, "idx_sessions_share") {
		t.Errorf("поиск по share_token не использует индекс: %s", detail)
	}
}

// EmptyShareTokenDoesNotMatchSessions — сессия без ссылки хранит пустой
// share_token, и запрос с пустым токеном не должен выбирать такую сессию:
// иначе публичная ссылка «без токена» показала бы чужой отчёт.
func TestEmptyShareTokenDoesNotMatchSessions(t *testing.T) {
	conn, dialect, err := Open("sqlite://:memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	ctx := context.Background()
	if err := Migrate(ctx, conn, dialect); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	sessions := NewSessionStore(conn, dialect)
	if _, err := sessions.Create(ctx, models.Session{
		UserID: 1, Grade: models.GradeMiddle, Stack: "go",
		Stage: models.StageVoice, Status: models.StatusActive,
		DurationLimitS: 1800, StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("session: %v", err)
	}
	if _, err := sessions.GetByShareToken(ctx, ""); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("пустой токен нашёл сессию: err = %v", err)
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
	// Резюме: описание + текст из файла + имя (валюта-минуты из схемы убрана).
	if err := store.SetResume(ctx, u.ID, Resume{About: "Go 5 лет", Text: "pdf-текст", FileName: "r.pdf", Path: "/tmp/1-x.pdf"}); err != nil {
		t.Fatalf("set resume: %v", err)
	}
	r, err := store.GetResume(ctx, u.ID)
	if err != nil {
		t.Fatalf("get resume: %v", err)
	}
	if r.About != "Go 5 лет" || r.Text != "pdf-текст" || r.FileName != "r.pdf" || r.Path != "/tmp/1-x.pdf" {
		t.Fatalf("resume: %+v", r)
	}
	if err := store.SetResume(ctx, u.ID, Resume{}); err != nil {
		t.Fatalf("reset resume: %v", err)
	}
	if r, err := store.GetResume(ctx, u.ID); err != nil || !r.Empty() {
		t.Fatalf("resume должен сбрасываться: %+v err %v", r, err)
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

// TestReportStagesColumn — issue #13: колонка stages переживает повторную
// миграцию, а старая строка без неё (колонки добавляются ALTER'ом поверх
// существующих БД) читается как '[]' и не ломает Get.
func TestReportStagesColumn(t *testing.T) {
	conn, dialect, err := Open("sqlite://:memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	ctx := context.Background()
	if err := Migrate(ctx, conn, dialect); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	users, sessions := NewUserStore(conn, dialect), NewSessionStore(conn, dialect)
	u, err := users.CreateUser(ctx, "stages@test.dev", "hash")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	sm, err := sessions.Create(ctx, models.Session{
		UserID: u.ID, Grade: models.GradeMiddle, Stack: "go",
		Stage: models.StageVoice, Status: models.StatusActive,
		DurationLimitS: 1800, StartedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	sid := sm.ID
	reports := NewReportStore(conn, dialect)

	// Имитация старой схемы: убираем колонку и пишем отчёт без неё (как
	// делал старый код), затем гоняем Migrate повторно — она обязана вернуть
	// колонку с дефолтом и за-fillить существующую строку.
	if _, err := conn.ExecContext(ctx, `ALTER TABLE reports DROP COLUMN stages`); err != nil {
		t.Fatalf("drop column (имитация старой схемы): %v", err)
	}
	if _, err := conn.ExecContext(ctx, dialect.q(`
		INSERT INTO reports (session_id, overall, grade_recommendation,
			criteria, strengths, weaknesses, recommendations, created_at)
		VALUES (?, 3.5, 'middle', '{}', '[]', '[]', '[]', '2026-01-01T00:00:00Z')`), sid); err != nil {
		t.Fatalf("insert старой строки: %v", err)
	}
	if err := Migrate(ctx, conn, dialect); err != nil {
		t.Fatalf("migrate повторно: %v", err)
	}
	if _, _, _, _, _, _, extra, err := reports.Get(ctx, sid); err != nil {
		t.Fatalf("get старой строки: %v", err)
	} else if extra.Stages != "[]" {
		t.Errorf("старая строка: stages = %q, want '[]'", extra.Stages)
	}

	// Новый отчёт: stages сохраняются и читаются как есть.
	stages := `[{"stage":"livecode","title":"Live-Code","score":4.5,"basis":"задач 2, сдано 2, пройдено 1"}]`
	err = reports.Save(ctx, sid, 4.0, "middle", []byte(`{}`), []byte(`[]`), []byte(`[]`), []byte(`[]`),
		ReportExtra{Stages: stages})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, _, _, _, _, _, extra, err := reports.Get(ctx, sid); err != nil {
		t.Fatalf("get: %v", err)
	} else if extra.Stages != stages {
		t.Errorf("stages = %q, want %q", extra.Stages, stages)
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
