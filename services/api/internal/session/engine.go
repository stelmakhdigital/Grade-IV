package session

import (
	"context"
	"errors"
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
	store     *db.SessionStore
	users     *db.UserStore
	templates *db.TemplateStore // итерация A: настраиваемые планы (может быть nil)
	log       *slog.Logger

	now           func() time.Time
	pauseTimeout  time.Duration
	sessionLimitS int // SESSION_LIMIT_S: >0 фикс., <0 без лимита, 0 по грейду

	mu   sync.Mutex
	rts  map[int64]*runtime
	stop chan struct{}
	done chan struct{}
}

// Opt — опция построения движка.
type Opt func(*Engine)

// WithNow подменяет источник времени (тесты).
func WithNow(f func() time.Time) Opt { return func(e *Engine) { e.now = f } }

// WithSessionLimit — глобальный лимит длительности сессии (SESSION_LIMIT_S):
// s > 0 — фиксированный лимит в секундах для всех сессий;
// s < 0 — без лимита (сессия не финализируется по времени, таймер не шлётся);
// s == 0 — дефолт: лимит по грейду (models.SessionDurationS).
func WithSessionLimit(s int) Opt { return func(e *Engine) { e.sessionLimitS = s } }

// WithPauseTimeout задаёт порог «обрыв без возобновления» (SRS §7).
func WithPauseTimeout(d time.Duration) Opt {
	return func(e *Engine) {
		if d > 0 {
			e.pauseTimeout = d
		}
	}
}

// WithTemplates — хранилище настраиваемых шаблонов (итерация A).
// При Create с templateID≠0 сессия получает программу шаблона; при templateID=0
// — дефолтный шаблон грейда (если хранилище задано).
func WithTemplates(ts *db.TemplateStore) Opt { return func(e *Engine) { e.templates = ts } }

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

// rt возвращает runtime по ID или nil.
func (e *Engine) rt(id int64) *runtime {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.rts[id]
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
