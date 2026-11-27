package session

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// Завершение сессии: переход на стадию report, финальный учёт активного времени
// и итоговые сообщения в WS. Вызывается по инициативе кандидата (Finish), при
// выходе на report (Transition) и по исчерпании лимита времени (tick).

// Finish завершает сессию по инициативе кандидата: переход на стадию report
// и итоговый учёт активного времени (в MVP он совпадает с выходом на report).
func (e *Engine) Finish(id int64) (*Snapshot, error) { return e.finish(id, true, "user") }

func (e *Engine) finish(id int64, byUser bool, reason string) (*Snapshot, error) {
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
	// spec-14/A5: БД под тем же замком — при ошибке записи память не мутируем
	// (двойной finish по гонке отсечён терминальной проверкой выше; память
	// не расходится с БД при рестарте после ошибки).
	if err := e.store.SetStage(ctx, id, models.StageReport); err != nil {
		rt.connMu.Unlock()
		return nil, err
	}
	if err := e.store.Finalize(ctx, id, models.StatusFinished, active, now); err != nil {
		rt.connMu.Unlock()
		return nil, err
	}
	rt.status = models.StatusFinished
	rt.stage = models.StageReport
	rt.since = time.Time{}
	rt.pausedAt = time.Time{}
	rt.lastPersist = now
	rt.connMu.Unlock()

	evt := map[string]any{"reason": reason, "active_seconds": math.Round(active*100) / 100}
	if byUser {
		evt["by_user"] = true
	}
	_, _ = e.addEvent(ctx, id, "finished", evt)
	e.sendJSON(id, map[string]any{"type": "timer", "remaining_s": 0})
	stageMsg := map[string]any{"type": "stage", "name": models.StageReport, "task": nil}
	// issue #10: track-мета нужна и здесь — UI ставит её безусловно
	// (m.track ?? null), и без неё на финале терялись имя роли в шапке и
	// подпись «Практика (bash)» в отчёте (BUG-20261104-026/-028).
	// rt.track/rt.grade/rt.stages после создания рантайма не меняются — чтение
	// вне connMu безопасно, как в Transition/ResendStage.
	if tm := trackStageMeta(rt); tm != nil {
		stageMsg["track"] = tm
	}
	e.sendJSON(id, stageMsg)
	return e.snapshotLocked(rt)
}
