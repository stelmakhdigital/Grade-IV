package session

import (
	"context"
	"fmt"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// Переходы между стадиями: Transition (валидация машиной состояний с учётом
// программы грейда/роли) и повтор текущей стадии при WS-reconnect.

// ResendStage — повтор текущей стадии (WS-reconnect): та же сборка сообщения,
// что и в Transition (spec-18: board в stage для design).
func (e *Engine) ResendStage(id int64) {
	rt := e.rt(id)
	if rt == nil {
		return
	}
	rt.connMu.Lock()
	stage := rt.stage
	msg := map[string]any{"type": "stage", "name": stage, "task": nil}
	if e.debugStages {
		msg["debug"] = true // UI показывает переключатель стадий (dev)
	}
	if tm := trackStageMeta(rt); tm != nil {
		msg["track"] = tm
	}
	if stage == models.StageDesign {
		if b := boardStagePayload(rt.track, rt.grade); b != nil {
			msg["board"] = b
		}
	}
	rt.connMu.Unlock()
	e.sendJSON(id, msg)
}

// Transition — смена стадии (валидация машиной состояний, с учётом программы грейда).
// С e.debugStages (ENABLE_DEBUG=1) допускается прыжок на любую стадию программы роли.
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
	prog := rt.stageProgram()
	if err := CanTransitionStage(prog, rt.stage, to); err != nil {
		if !e.debugStages || CanJumpStageDebug(prog, to) != nil {
			rt.connMu.Unlock()
			return nil, err
		}
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
	msg := map[string]any{"type": "stage", "name": to, "task": nil}
	if e.debugStages {
		msg["debug"] = true // UI показывает переключатель стадий (dev)
	}
	// spec-18/Ф3: конфиг доски идёт в stage-сообщении (палитра/заголовок/кейс).
	if to == models.StageDesign {
		if b := boardStagePayload(rt.track, rt.grade); b != nil {
			msg["board"] = b
		}
	}
	// spec-20: метаданные трека в КАЖДОМ stage-сообщении (ResendStage шлёт их
	// только при реконнекте — после перехода стадии UI мог остаться без них).
	if tm := trackStageMeta(rt); tm != nil {
		msg["track"] = tm
	}
	e.sendJSON(id, msg)
	return e.snapshotLocked(rt)
}
