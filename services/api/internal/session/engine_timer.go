package session

import (
	"context"
	"math"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

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
		// Лимит времени только при limitS > 0 (SESSION_LIMIT_S < 0 — без
		// ограничения: финализация по времени и timer-сообщения отключены).
		if rt.limitS > 0 && remaining <= 0 {
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
		if rt.limitS > 0 && conn != nil && now.Sub(rt.lastTimer) >= TimerBroadcast {
			rt.lastTimer = now
			rt.connMu.Unlock()
			e.sendJSON(id, map[string]any{"type": "timer", "remaining_s": remaining})
			continue
		}
		rt.connMu.Unlock()
	}
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

// clamp0 — max(0, n).
func clamp0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}
