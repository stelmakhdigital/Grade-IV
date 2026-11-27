package session

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// Управление статусом сессии: пауза, возобновление и принудительное прерывание,
// с учётом активного времени, записью в БД и отправкой событий.

// Pause ставит активную сессию в паузу (учёт активного времени останавливается).
func (e *Engine) Pause(id int64) (*Snapshot, error) { return e.pause(id, "user") }

func (e *Engine) pause(id int64, reason string) (*Snapshot, error) {
	rt := e.rt(id)
	if rt == nil {
		if rt = e.reconstructFromStore(id); rt == nil { // spec-14/A4: после рестарта
			return nil, fmt.Errorf("%w: %d", ErrNotAttached, id)
		}
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
		if rt = e.reconstructFromStore(id); rt == nil { // spec-14/A4: после рестарта
			return nil, fmt.Errorf("%w: %d", ErrNotAttached, id)
		}
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

// abort — принудительное завершение (пауза дольше порога); учитывается
// фактическое активное время.
func (e *Engine) abort(id int64, reason string) (*Snapshot, error) {
	rt := e.rt(id)
	if rt == nil {
		if rt = e.reconstructFromStore(id); rt == nil { // spec-14/A4: после рестарта
			return nil, fmt.Errorf("%w: %d", ErrNotAttached, id)
		}
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
	active := rt.active
	// spec-14/A5: БД под тем же замком — при ошибке записи память не мутируем.
	if err := e.store.Finalize(ctx, id, models.StatusAborted, active, now); err != nil {
		rt.connMu.Unlock()
		return nil, err
	}
	rt.status = models.StatusAborted
	rt.since = time.Time{}
	rt.pausedAt = time.Time{}
	rt.lastPersist = now
	rt.connMu.Unlock()

	_, _ = e.addEvent(ctx, id, "aborted", map[string]any{
		"reason": reason, "active_seconds": math.Round(active*100) / 100,
	})
	e.sendJSON(id, map[string]any{"type": "error", "code": "session_aborted", "msg": "сессия прервана"})
	return e.snapshotLocked(rt)
}
