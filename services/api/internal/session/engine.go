package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
	"nhooyr.io/websocket"
)

// Ключевые пороги (SRS §7: численные пороги конфигурируемые).
const (
	// TimerBroadcast — период отправки timer-сообщений по WS (spec: 1 раз/5 с).
	TimerBroadcast = 5 * time.Second
	// tickPeriod — период внутреннего тика таймера.
	tickPeriod = time.Second
	// DefaultPauseTimeout — обрыв без возобновления дольше порога → финализация (SRS §7).
	DefaultPauseTimeout = 30 * time.Minute
)

// Ошибки движка.
var (
	ErrNoMinutes     = errors.New("нет доступных минут")
	ErrSessionEnded  = errors.New("сессия завершена")
	ErrNotAttached   = errors.New("сессия не найдена в движке")
	ErrInvalidParams = errors.New("некорректные параметры")
)

// Snapshot — актуальное состояние сессии для REST/WS.
type Snapshot struct {
	ID             int64
	UserID         int64
	Grade          models.Grade
	Stack          models.Stack
	Stage          models.Stage
	Status         models.Status
	DurationLimitS int
	ActiveSeconds  float64
	TimeLeftS      int
	StartedAt      time.Time
	FinishedAt     *time.Time
	PausedAt       *time.Time
}

// runtime — живое состояние сессии в памяти (зеркалируется в БД на переходах и тиках).
type runtime struct {
	userID   int64
	grade    models.Grade
	stack    models.Stack
	limitS   int
	stage    models.Stage
	status   models.Status
	active   float64 // накопленные активные секунды (включая текущий интервал)
	since    time.Time
	pausedAt time.Time

	conn        *websocket.Conn
	connMu      sync.Mutex // правило nhooyr: один писатель на соединение
	lastPersist time.Time
	lastTimer   time.Time
}

// Engine — оркестратор сессий: машина состояний, тарификация, таймер, WS-регистрация.
type Engine struct {
	store *db.SessionStore
	users *db.UserStore
	log   *slog.Logger

	now          func() time.Time
	pauseTimeout time.Duration

	mu   sync.Mutex
	rts  map[int64]*runtime
	stop chan struct{}
	done chan struct{}
}

// Opt — опция построения движка.
type Opt func(*Engine)

// WithNow подменяет источник времени (тесты).
func WithNow(f func() time.Time) Opt { return func(e *Engine) { e.now = f } }

// WithPauseTimeout задаёт порок «обрыв без возобновления» (SRS §7).
func WithPauseTimeout(d time.Duration) Opt {
	return func(e *Engine) {
		if d > 0 {
			e.pauseTimeout = d
		}
	}
}

// New создаёт движок.
func New(store *db.SessionStore, users *db.UserStore, log *slog.Logger, opts ...Opt) *Engine {
	e := &Engine{
		store:        store,
		users:        users,
		log:          log,
		now:          time.Now,
		pauseTimeout: DefaultPauseTimeout,
		rts:          make(map[int64]*runtime),
		stop:         make(chan struct{}),
		done:         make(chan struct{}),
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

// Run запускает цикл тика; блокируется до Stop (или завершения ctx).
// При старте выполняет рекавери: сессии со статусом active в БД считаются
// оборванными (FR-S7) и переводятся в paused — активное время уже сохранено
// последним тиком (потеря ≤ TimerBroadcast).
func (e *Engine) Run(ctx context.Context) {
	defer close(e.done)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	active, err := e.store.ListByStatus(ctx, models.StatusActive)
	if err != nil {
		e.log.Error("recovery: список активных сессий", "err", err)
	}
	for _, m := range active {
		e.log.Warn("рекавери: сессия была активной на старте — ставлю в паузу", "session", m.ID)
		if err := e.store.MarkPaused(ctx, m.ID, m.ActiveSeconds, e.now()); err != nil {
			e.log.Error("рекавери: пауза", "session", m.ID, "err", err)
			continue
		}
		_, _ = e.addEvent(ctx, m.ID, "paused", map[string]any{
			"reason": "service_restarted", "active_seconds": m.ActiveSeconds,
		})
	}

	t := time.NewTicker(tickPeriod)
	defer t.Stop()
	for {
		select {
		case <-e.stop:
			return
		case <-ctx.Done():
			return
		case <-t.C:
			e.tick(e.now())
		}
	}
}

// Stop останавливает цикл тика.
func (e *Engine) Stop() {
	select {
	case <-e.stop:
	default:
		close(e.stop)
	}
	<-e.done
}

// tick — секундный ход таймера: начисление времени, контроль лимита, timer-сообщения.
func (e *Engine) tick(now time.Time) {
	e.mu.Lock()
	ids := make([]int64, 0, len(e.rts))
	for id := range e.rts {
		ids = append(ids, id)
	}
	e.mu.Unlock()

	for _, id := range ids {
		rt := e.rt(id)
		if rt == nil {
			continue
		}
		rt.connMu.Lock()
		if rt.status != models.StatusActive {
			rt.connMu.Unlock()
			continue
		}
		// Сводим интервал в накопление и сбрасаем since: следующий тик/финализация
		// не начислит его повторно.
		rt.active += now.Sub(rt.since).Seconds()
		rt.since = now
		remaining := rt.limitS - int(rt.active)
		conn := rt.conn
		if remaining <= 0 {
			rt.connMu.Unlock()
			e.log.Info("доставлен лимит времени сессии — финализация", "session", id)
			_, _ = e.finish(id, false, "time_limit")
			continue
		}
		if now.Sub(rt.lastPersist) >= TimerBroadcast {
			rt.lastPersist = now
			go func() {
				_ = e.store.PersistActiveSeconds(context.Background(), id, rt.active)
			}()
		}
		if conn != nil && now.Sub(rt.lastTimer) >= TimerBroadcast {
			rt.lastTimer = now
			rt.connMu.Unlock()
			e.sendJSON(id, map[string]any{"type": "timer", "remaining_s": remaining})
			continue
		}
		rt.connMu.Unlock()
	}
}

// Create создаёт сессию: проверка минут (402-условие), вставка, старт в статусе active.
func (e *Engine) Create(ctx context.Context, userID int64, grade models.Grade, stack models.Stack) (models.Session, error) {
	if !grade.Valid() {
		return models.Session{}, fmt.Errorf("%w: грейд %q", ErrInvalidParams, grade)
	}
	if !stack.Valid() {
		return models.Session{}, fmt.Errorf("%w: стек %q", ErrInvalidParams, stack)
	}
	minutes, err := e.users.MinutesRemaining(ctx, userID)
	if err != nil {
		return models.Session{}, err
	}
	if minutes <= 0 {
		return models.Session{}, ErrNoMinutes
	}
	now := e.now()
	m, err := e.store.Create(ctx, models.Session{
		UserID:         userID,
		Grade:          grade,
		Stack:          stack,
		Stage:          models.StageVoice,
		Status:         models.StatusActive,
		DurationLimitS: models.SessionDurationS(grade),
		StartedAt:      now,
	})
	if err != nil {
		return models.Session{}, err
	}
	e.mu.Lock()
	e.rts[m.ID] = &runtime{
		userID:      userID,
		grade:       grade,
		stack:       stack,
		limitS:      m.DurationLimitS,
		stage:       models.StageVoice,
		status:      models.StatusActive,
		since:       now,
		lastPersist: now,
	}
	e.mu.Unlock()
	_, _ = e.addEvent(ctx, m.ID, "session_created", map[string]any{
		"grade": grade, "stack": stack, "duration_limit_s": m.DurationLimitS,
	})
	return m, nil
}

// Attach регистрирует WS-соединение сессии (вызывается WS-хендлером после auth).
func (e *Engine) Attach(ctx context.Context, id, userID int64, conn *websocket.Conn) (*Snapshot, error) {
	m, err := e.store.GetOwned(ctx, id, userID)
	if err != nil {
		return nil, err
	}
	if IsTerminalStatus(m.Status) {
		return nil, fmt.Errorf("%w: %s", ErrSessionEnded, m.Status)
	}
	e.mu.Lock()
	rt, ok := e.rts[id]
	if !ok {
		// Нет живого состояния (например, сессия создана другим экземпляром/до старта
		// движка): реконструируем из БД. Активная без since — консервативно в паузу.
		rt = &runtime{
			userID:      userID,
			grade:       m.Grade,
			stack:       m.Stack,
			limitS:      m.DurationLimitS,
			stage:       m.Stage,
			status:      m.Status,
			active:      m.ActiveSeconds,
			lastPersist: time.Now(),
			lastTimer:   time.Now().Add(-TimerBroadcast), // сразу выдать timer
		}
		if m.Status == models.StatusActive {
			e.log.Warn("reconstruct: сессия active без живого таймера — ставлю в паузу", "session", id)
			if err := e.store.MarkPaused(context.Background(), id, m.ActiveSeconds, e.now()); err == nil {
				rt.status = models.StatusPaused
				rt.pausedAt = e.now()
			}
		} else {
			rt.pausedAt = e.now()
			if m.PausedAt != nil {
				rt.pausedAt = *m.PausedAt
			}
		}
		e.rts[id] = rt
	}
	rt.connMu.Lock()
	rt.conn = conn
	rt.connMu.Unlock()
	e.mu.Unlock()
	return e.snapshotLocked(rt)
}

// Detach снимает WS-соединение; активная сессия ставится в паузу (FR-S7: обрыв → paused).
func (e *Engine) Detach(id int64) {
	rt := e.rt(id)
	if rt == nil {
		return
	}
	rt.connMu.Lock()
	wasActive := rt.status == models.StatusActive
	rt.conn = nil
	rt.connMu.Unlock()
	if wasActive {
		_, _ = e.pause(id, "ws_disconnected")
	}
}

// rt возвращает runtime по ID или nil.
func (e *Engine) rt(id int64) *runtime {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.rts[id]
}

// Pause ставит активную сессию в паузу (тарификация останавливается).
func (e *Engine) Pause(id int64) (*Snapshot, error) { return e.pause(id, "user") }

func (e *Engine) pause(id int64, reason string) (*Snapshot, error) {
	rt := e.rt(id)
	if rt == nil {
		return nil, fmt.Errorf("%w: %d", ErrNotAttached, id)
	}
	ctx := context.Background()
	rt.connMu.Lock()
	now := e.now()
	if rt.status == models.StatusActive {
		rt.active += now.Sub(rt.since).Seconds()
	}
	if rt.status != models.StatusActive {
		rt.connMu.Unlock()
		return nil, fmt.Errorf("%w: %s → paused", ErrIllegalStatus, rt.status)
	}
	rt.status = models.StatusPaused
	rt.pausedAt = now
	rt.since = time.Time{}
	active := rt.active
	rt.lastPersist = now
	rt.connMu.Unlock()

	if err := e.store.MarkPaused(ctx, id, active, now); err != nil {
		return nil, err
	}
	_, _ = e.addEvent(ctx, id, "paused", map[string]any{
		"reason": reason, "active_seconds": math.Round(active*100) / 100,
	})
	e.sendJSON(id, map[string]any{"type": "timer", "remaining_s": clamp0(rt.limitS - int(active))})
	return e.snapshotLocked(rt)
}

// Resume снимает сессию с паузы. Пауза дольше порога → aborted (SRS §7).
func (e *Engine) Resume(id int64) (*Snapshot, error) {
	rt := e.rt(id)
	if rt == nil {
		return nil, fmt.Errorf("%w: %d", ErrNotAttached, id)
	}
	ctx := context.Background()
	rt.connMu.Lock()
	if rt.status != models.StatusPaused {
		cur := rt.status
		rt.connMu.Unlock()
		return nil, fmt.Errorf("%w: %s → active", ErrIllegalStatus, cur)
	}
	if e.now().Sub(rt.pausedAt) > e.pauseTimeout {
		rt.connMu.Unlock()
		e.log.Info("пауза дольше порога — сессия прервана", "session", id, "paused_at", rt.pausedAt)
		return e.abort(id, "pause_timeout")
	}
	now := e.now()
	rt.status = models.StatusActive
	rt.since = now
	rt.pausedAt = time.Time{}
	rt.lastTimer = now.Add(-TimerBroadcast)
	rt.lastPersist = now
	rt.connMu.Unlock()

	if err := e.store.MarkActive(ctx, id); err != nil {
		return nil, err
	}
	_, _ = e.addEvent(ctx, id, "resumed", map[string]any{})
	e.sendJSON(id, map[string]any{"type": "timer", "remaining_s": clamp0(rt.limitS - int(rt.active))})
	return e.snapshotLocked(rt)
}

// Finish завершает сессию по инициативе кандидата: переход на стадию report
// и финализация тарификации (SRS §7: тарификация — до сгенерированного отчёта,
// в MVP финализация совпадает с выходом на report).
func (e *Engine) Finish(id int64) (*Snapshot, error) { return e.finish(id, true, "user") }

func (e *Engine) finish(id int64, byUser bool, reason string) (*Snapshot, error) {
	rt := e.rt(id)
	if rt == nil {
		return nil, fmt.Errorf("%w: %d", ErrNotAttached, id)
	}
	ctx := context.Background()
	rt.connMu.Lock()
	if IsTerminalStatus(rt.status) {
		cur := rt.status
		rt.connMu.Unlock()
		return nil, fmt.Errorf("%w: %s", ErrSessionEnded, cur)
	}
	now := e.now()
	if rt.status == models.StatusActive {
		rt.active += now.Sub(rt.since).Seconds()
	}
	rt.status = models.StatusFinished
	rt.stage = models.StageReport
	rt.since = time.Time{}
	rt.pausedAt = time.Time{}
	active := rt.active
	rt.lastPersist = now
	rt.connMu.Unlock()

	if err := e.store.SetStage(ctx, id, models.StageReport); err != nil {
		return nil, err
	}
	if err := e.store.Finalize(ctx, id, models.StatusFinished, active, now); err != nil {
		return nil, err
	}
	e.bill(ctx, rt, id, active, "session_finished")
	evt := map[string]any{"reason": reason, "active_seconds": math.Round(active*100) / 100}
	if byUser {
		evt["by_user"] = true
	}
	_, _ = e.addEvent(ctx, id, "finished", evt)
	e.sendJSON(id, map[string]any{"type": "timer", "remaining_s": 0})
	e.sendJSON(id, map[string]any{"type": "stage", "name": models.StageReport, "task": nil})
	return e.snapshotLocked(rt)
}

// abort — принудительное завершение (пауза дольше порога); тарифицируется
// фактическое активное время (SRS §7).
func (e *Engine) abort(id int64, reason string) (*Snapshot, error) {
	rt := e.rt(id)
	if rt == nil {
		return nil, fmt.Errorf("%w: %d", ErrNotAttached, id)
	}
	ctx := context.Background()
	rt.connMu.Lock()
	if IsTerminalStatus(rt.status) {
		cur := rt.status
		rt.connMu.Unlock()
		return nil, fmt.Errorf("%w: %s", ErrSessionEnded, cur)
	}
	now := e.now()
	if rt.status == models.StatusActive {
		rt.active += now.Sub(rt.since).Seconds()
	}
	rt.status = models.StatusAborted
	rt.since = time.Time{}
	rt.pausedAt = time.Time{}
	active := rt.active
	rt.lastPersist = now
	rt.connMu.Unlock()

	if err := e.store.Finalize(ctx, id, models.StatusAborted, active, now); err != nil {
		return nil, err
	}
	e.bill(ctx, rt, id, active, "session_aborted")
	_, _ = e.addEvent(ctx, id, "aborted", map[string]any{
		"reason": reason, "active_seconds": math.Round(active*100) / 100,
	})
	e.sendJSON(id, map[string]any{"type": "error", "code": "session_aborted", "msg": "сессия прервана"})
	return e.snapshotLocked(rt)
}

// Transition — смена стадии (валидация машиной состояний, с учётом программы грейда).
func (e *Engine) Transition(id int64, to models.Stage) (*Snapshot, error) {
	rt := e.rt(id)
	if rt == nil {
		return nil, fmt.Errorf("%w: %d", ErrNotAttached, id)
	}
	rt.connMu.Lock()
	if IsTerminalStatus(rt.status) {
		cur := rt.status
		from := rt.stage
		rt.connMu.Unlock()
		return nil, fmt.Errorf("%w: %s (стадия %s)", ErrSessionEnded, cur, from)
	}
	if err := CanTransitionStage(rt.grade, rt.stage, to); err != nil {
		rt.connMu.Unlock()
		return nil, err
	}
	from := rt.stage
	rt.stage = to
	rt.connMu.Unlock()

	ctx := context.Background()
	if err := e.store.SetStage(ctx, id, to); err != nil {
		return nil, err
	}
	_, _ = e.addEvent(ctx, id, "stage_change", map[string]any{"from": from, "to": to})
	if to == models.StageReport {
		return e.finish(id, true, "stage_report")
	}
	e.sendJSON(id, map[string]any{"type": "stage", "name": to, "task": nil})
	return e.snapshotLocked(rt)
}

// bill — проводка расхода минут: delta = −округлённые активные секунды.
func (e *Engine) bill(ctx context.Context, rt *runtime, sessionID int64, active float64, reason string) {
	delta := -int(math.Round(active))
	if delta == 0 {
		return
	}
	if err := e.users.GrantMinutes(ctx, rt.userID, &sessionID, delta, reason); err != nil {
		e.log.Error("ledger: проводка расхода минут", "session", sessionID, "err", err)
	}
}

// remainingS — остаток времени сессии, с (для terminal — 0). Требует rt.connMu.
func (e *Engine) remainingSLocked(rt *runtime) int {
	if IsTerminalStatus(rt.status) {
		return 0
	}
	rem := rt.limitS - int(e.currentActiveLocked(rt))
	if rem < 0 {
		rem = 0
	}
	return rem
}

// Snapshot — снимок состояния по ID (для REST GET).
func (e *Engine) Snapshot(id int64) (*Snapshot, error) {
	rt := e.rt(id)
	if rt != nil {
		return e.snapshotLocked(rt)
	}
	// Сессия без живого рантайма (чужой экземпляр/история): только из БД.
	ctx := context.Background()
	m, err := e.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return &Snapshot{
		ID: m.ID, UserID: m.UserID, Grade: m.Grade, Stack: m.Stack,
		Stage: m.Stage, Status: m.Status, DurationLimitS: m.DurationLimitS,
		ActiveSeconds: m.ActiveSeconds, TimeLeftS: 0,
		StartedAt: m.StartedAt, FinishedAt: m.FinishedAt, PausedAt: m.PausedAt,
	}, nil
}

func (e *Engine) snapshotLocked(rt *runtime) (*Snapshot, error) {
	rt.connMu.Lock()
	defer rt.connMu.Unlock()
	return &Snapshot{
		Grade:          rt.grade,
		Stack:          rt.stack,
		Stage:          rt.stage,
		Status:         rt.status,
		DurationLimitS: rt.limitS,
		ActiveSeconds:  math.Round(e.currentActiveLocked(rt)*100) / 100,
		TimeLeftS:      e.remainingSLocked(rt),
	}, nil
}

// currentActiveLocked — активные секунды с учётом текущего интервала. Требует rt.connMu.
func (e *Engine) currentActiveLocked(rt *runtime) float64 {
	if rt.status == models.StatusActive {
		return rt.active + e.now().Sub(rt.since).Seconds()
	}
	return rt.active
}

// addEvent — событие сессии (data сериализуется в JSON).
func (e *Engine) addEvent(ctx context.Context, sessionID int64, kind string, data map[string]any) (int, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return 0, err
	}
	return e.store.AddEvent(ctx, sessionID, kind, raw)
}

// clamp0 — max(0, n).
func clamp0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

// SendTo — публичная отправка JSON-сообщения клиенту сессии (безопасно: один писатель).
func (e *Engine) SendTo(id int64, v any) { e.sendJSON(id, v) }

// sendJSON — отправка JSON-сообщения клиенту (безопасно: один писатель).
func (e *Engine) sendJSON(id int64, v any) {
	rt := e.rt(id)
	if rt == nil {
		return
	}
	rt.connMu.Lock()
	defer rt.connMu.Unlock()
	if rt.conn == nil {
		return
	}
	raw, err := json.Marshal(v)
	if err != nil {
		e.log.Warn("ws: сериализация", "session", id, "err", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rt.conn.Write(ctx, websocket.MessageText, raw); err != nil {
		e.log.Warn("ws: ошибка записи", "session", id, "err", err)
	}
}
