package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// testEnv — изолированная среда: sqlite :memory:, пользователь с грантом минут, движок с фейковым временем.
type testEnv struct {
	t      *testing.T
	engine *Engine
	users  *db.UserStore
	store  *db.SessionStore
	userID int64
	now    *time.Time
}

func newTestEnv(t *testing.T, grantS int) *testEnv {
	t.Helper()
	conn, dialect, err := db.Open("sqlite://:memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx, conn, dialect); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return newTestEnvWithDB(t, conn, dialect, grantS)
}

// newTestEnvWithDB — среда поверх общей (уже смигрированной) БД; пользователь создаётся с уникальным email.
func newTestEnvWithDB(t *testing.T, conn *sql.DB, dialect db.Dialect, grantS int) *testEnv {
	t.Helper()
	ctx := context.Background()
	users := db.NewUserStore(conn, dialect)
	store := db.NewSessionStore(conn, dialect)

	u, err := users.CreateUser(ctx, fmt.Sprintf("tester-%d@example.com", time.Now().UnixNano()), "test-hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if grantS > 0 {
		if err := users.GrantMinutes(ctx, u.ID, nil, grantS, "test_grant"); err != nil {
			t.Fatalf("grant: %v", err)
		}
	}
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	eng := New(store, users, slog.New(slog.DiscardHandler),
		WithNow(func() time.Time { return now }),
		WithPauseTimeout(DefaultPauseTimeout))
	return &testEnv{t: t, engine: eng, users: users, store: store, userID: u.ID, now: &now}
}

// advance двигает фейковые часы на d.
func (e *testEnv) advance(d time.Duration) {
	*e.now = e.now.Add(d)
}

func minutesRemaining(t *testing.T, e *testEnv) int64 {
	t.Helper()
	m, err := e.users.MinutesRemaining(context.Background(), e.userID)
	if err != nil {
		t.Fatalf("minutes: %v", err)
	}
	return m
}

func TestCreateSession(t *testing.T) {
	e := newTestEnv(t, 3600)
	ctx := context.Background()

	m, err := e.engine.Create(ctx, e.userID, models.GradeJunior, models.StackGo)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if m.Stage != models.StageVoice || m.Status != models.StatusActive {
		t.Fatalf("start: stage=%s status=%s", m.Stage, m.Status)
	}
	if m.DurationLimitS != 45*60 {
		t.Fatalf("duration limit: %d", m.DurationLimitS)
	}
	if got := minutesRemaining(t, e); got != 3600 {
		t.Fatalf("минуты не должны списываться до финализации: %d", got)
	}
	snap, err := e.engine.Snapshot(m.ID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap.TimeLeftS != 45*60 {
		t.Fatalf("time left: %d", snap.TimeLeftS)
	}
	events, _ := e.store.ListEvents(ctx, m.ID)
	if len(events) != 1 || events[0].Kind != "session_created" {
		t.Fatalf("события: %+v", events)
	}
}

func TestCreateSessionInvalidParams(t *testing.T) {
	e := newTestEnv(t, 3600)
	if _, err := e.engine.Create(context.Background(), e.userID, models.Grade("intern"), models.StackGo); err == nil {
		t.Error("ожидалась ошибка: неизвестный грейд")
	}
	if _, err := e.engine.Create(context.Background(), e.userID, models.GradeJunior, models.Stack("rust")); err == nil {
		t.Error("ожидалась ошибка: стек вне MVP")
	}
}

func TestCreateSessionNoMinutes(t *testing.T) {
	e := newTestEnv(t, 0)
	_, err := e.engine.Create(context.Background(), e.userID, models.GradeJunior, models.StackGo)
	if !errors.Is(err, ErrNoMinutes) {
		t.Fatalf("ожидался ErrNoMinutes, получено: %v", err)
	}
}

func TestPauseResumeBilling(t *testing.T) {
	e := newTestEnv(t, 3600)
	ctx := context.Background()
	m, err := e.engine.Create(ctx, e.userID, models.GradeMiddle, models.StackPython)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	e.advance(100 * time.Second)
	snap, err := e.engine.Pause(m.ID)
	if err != nil {
		t.Fatalf("pause: %v", err)
	}
	if snap.Status != models.StatusPaused {
		t.Fatalf("status: %s", snap.Status)
	}
	if got := snap.ActiveSeconds; got < 99.9 || got > 100.1 {
		t.Fatalf("active после pause: %v", got)
	}

	// Пауза не тарифицируется.
	e.advance(5 * time.Minute)
	snap, err = e.engine.Resume(m.ID)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if snap.Status != models.StatusActive {
		t.Fatalf("status после resume: %s", snap.Status)
	}
	if got := snap.ActiveSeconds; got < 99.9 || got > 100.1 {
		t.Fatalf("active не должно расти в паузе: %v", got)
	}

	e.advance(100 * time.Second)
	snap, err = e.engine.Finish(m.ID)
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if snap.Status != models.StatusFinished || snap.Stage != models.StageReport {
		t.Fatalf("final: %s/%s", snap.Status, snap.Stage)
	}
	if got := snap.ActiveSeconds; got < 199.9 || got > 200.1 {
		t.Fatalf("active финальное: %v", got)
	}
	if got := minutesRemaining(t, e); got != 3600-200 {
		t.Fatalf("ledger: %d (ожидалось 3400)", got)
	}
}

func TestPauseTimeoutAborts(t *testing.T) {
	e := newTestEnv(t, 3600)
	ctx := context.Background()
	m, err := e.engine.Create(ctx, e.userID, models.GradeJunior, models.StackGo)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	e.advance(30 * time.Second)
	if _, err := e.engine.Pause(m.ID); err != nil {
		t.Fatalf("pause: %v", err)
	}
	e.advance(DefaultPauseTimeout + time.Minute) // обрыв без возобновления
	snap, err := e.engine.Resume(m.ID)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if snap.Status != models.StatusAborted {
		t.Fatalf("ожидался aborted, получено: %s", snap.Status)
	}
	if got := minutesRemaining(t, e); got != 3600-30 {
		t.Fatalf("тарифицируется фактическое активное время: %d", got)
	}
}

func TestTimeLimitAutoFinish(t *testing.T) {
	e := newTestEnv(t, 3600)
	ctx := context.Background()
	m, err := e.engine.Create(ctx, e.userID, models.GradeJunior, models.StackGo) // лимит 2700 с
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	e.advance(2700 * time.Second)
	e.engine.tick(*e.now) // ручной тик вместо ожидания

	snap, err := e.engine.Snapshot(m.ID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap.Status != models.StatusFinished || snap.TimeLeftS != 0 {
		t.Fatalf("автонавершение по лимиту: %s left=%d", snap.Status, snap.TimeLeftS)
	}
	if got := minutesRemaining(t, e); got != 3600-2700 {
		t.Fatalf("ledger: %d", got)
	}
	// После финализации управление недоступно.
	if _, err := e.engine.Resume(m.ID); err == nil {
		t.Error("resume завершённой сессии должен ошибаться")
	}
}

func TestTransitions(t *testing.T) {
	e := newTestEnv(t, 3600)
	ctx := context.Background()
	m, err := e.engine.Create(ctx, e.userID, models.GradeMiddle, models.StackGo)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := e.engine.Transition(m.ID, models.StageLiveCode); err != nil {
		t.Fatalf("voice→livecode: %v", err)
	}
	if _, err := e.engine.Transition(m.ID, models.StageDesign); err != nil {
		t.Fatalf("livecode→design: %v", err)
	}
	if _, err := e.engine.Transition(m.ID, models.StageVoice); err == nil {
		t.Error("design→voice — перескок назад больше чем на 1")
	}
	if _, err := e.engine.Transition(m.ID, models.StageLiveCode); err != nil {
		t.Fatalf("возврат design→livecode: %v", err)
	}
	if _, err := e.engine.Transition(m.ID, models.StageDesign); err != nil {
		t.Fatalf("повторный livecode→design: %v", err)
	}
	snap, err := e.engine.Transition(m.ID, models.StageReport)
	if err != nil {
		t.Fatalf("design→report: %v", err)
	}
	if snap.Status != models.StatusFinished {
		t.Fatalf("report финализирует сессию: %s", snap.Status)
	}
}

func TestJuniorHasNoDesign(t *testing.T) {
	e := newTestEnv(t, 3600)
	ctx := context.Background()
	m, err := e.engine.Create(ctx, e.userID, models.GradeJunior, models.StackPython)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := e.engine.Transition(m.ID, models.StageLiveCode); err != nil {
		t.Fatalf("voice→livecode: %v", err)
	}
	if _, err := e.engine.Transition(m.ID, models.StageDesign); err == nil {
		t.Error("у Junior нет стадии System Design")
	}
	if _, err := e.engine.Transition(m.ID, models.StageReport); err != nil {
		t.Fatalf("livecode→report (Junior): %v", err)
	}
}

func TestDetachPausesActiveSession(t *testing.T) {
	e := newTestEnv(t, 3600)
	ctx := context.Background()
	m, err := e.engine.Create(ctx, e.userID, models.GradeMiddle, models.StackGo)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	e.advance(10 * time.Second)

	e.engine.Detach(m.ID) // имитация обрыва WS

	snap, err := e.engine.Snapshot(m.ID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap.Status != models.StatusPaused {
		t.Fatalf("обрыв → paused, получено: %s", snap.Status)
	}
}

func TestRecoveryPutsActiveSessionsOnPause(t *testing.T) {
	conn, dialect, err := db.Open("sqlite://:memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer conn.Close()
	if err := db.Migrate(context.Background(), conn, dialect); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	env1 := newTestEnvWithDB(t, conn, dialect, 3600)
	ctx := context.Background()
	m, err := env1.engine.Create(ctx, env1.userID, models.GradeMiddle, models.StackGo)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// «Перезапуск»: новый движок на той же БД; Run должен поставить active-сессии в паузу.
	env2 := newTestEnvWithDB(t, conn, dialect, 0)
	runCtx, cancel := context.WithCancel(context.Background())
	go env2.engine.Run(runCtx)
	defer cancel()
	// Ждём завершения рекавери: статус станет paused.
	deadline := time.Now().Add(2 * time.Second)
	for {
		s, err := env2.store.Get(ctx, m.ID)
		if err == nil && s.Status == models.StatusPaused {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("рекавери не перевёл сессию в paused: %v (err=%v)", s, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	env2.engine.Stop()
}
