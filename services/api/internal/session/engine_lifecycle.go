package session

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
	"nhooyr.io/websocket"
)

// Create создаёт сессию: проверка минут (402-условие), вставка, старт в статусе active.
func (e *Engine) Create(ctx context.Context, userID int64, grade models.Grade, stack models.Stack) (models.Session, error) {
	return e.CreateWithTemplate(ctx, userID, grade, stack, 0)
}

// CreateWithTemplate — создание сессии с шаблоном (итерация A).
// templateID=0 → дефолтный шаблон грейда (если templates задан); без templates
// — программа gradeProgram (как раньше). Program фиксируется в сессии.
func (e *Engine) CreateWithTemplate(ctx context.Context, userID int64, grade models.Grade, stack models.Stack, templateID int64) (models.Session, error) {
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
	limit := models.SessionDurationS(grade)
	switch {
	case e.sessionLimitS > 0:
		limit = e.sessionLimitS // SESSION_LIMIT_S — фиксированный лимит, с
	case e.sessionLimitS < 0:
		limit = 0 // SESSION_LIMIT_S=0/off — без ограничения по времени
	}
	// Шаблон: явный id или дефолтный грейда (итерация A).
	var program string
	if e.templates != nil {
		var tpl *db.Template
		if templateID > 0 {
			tpl, err = e.templates.Get(ctx, templateID)
			if err != nil {
				return models.Session{}, err
			}
			if tpl == nil || tpl.Grade != grade {
				return models.Session{}, fmt.Errorf("%w: шаблон %d не для грейда %s", ErrInvalidParams, templateID, grade)
			}
		} else {
			tpl, err = e.templates.Default(ctx, grade, string(stack))
			if err != nil {
				return models.Session{}, err
			}
		}
		if tpl != nil {
			program = tpl.ProgramString()
			templateID = tpl.ID
		}
	}
	m, err := e.store.Create(ctx, models.Session{
		UserID:         userID,
		Grade:          grade,
		Stack:          stack,
		Stage:          models.StageVoice,
		Status:         models.StatusActive,
		DurationLimitS: limit,
		StartedAt:      now,
		TemplateID:     templateID,
		Program:        program,
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
