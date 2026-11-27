package session

import (
	"context"
	"fmt"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
	"nhooyr.io/websocket"
)

// Подключение к сессии: регистрация WS-соединения (Attach/Detach) и
// реконструкция runtime из БД, когда живого состояния нет (рестарт сервиса,
// другой экземпляр движка).

// resolveTrack — роль (трек) сессии по track_id из БД. Резолвер отдаёт
// (nil, nil) для пустого/неизвестного id → прежнее поведение: программа стадий,
// payload доски и мета стадии считаются по легаси-правилу грейда.
func (e *Engine) resolveTrack(trackID string) *models.Track {
	if e.tracks == nil {
		return nil
	}
	t, err := e.tracks(trackID)
	if err != nil {
		e.log.Warn("reconstruct: роль не разрешена — программа по грейду", "track_id", trackID, "err", err)
		return nil
	}
	return t
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
	track := e.resolveTrack(m.TrackID) // issue #1: роль терялась при реконструкции
	e.mu.Lock()
	rt, ok := e.rts[id]
	if !ok {
		// Нет живого состояния (например, сессия создана другим экземпляром/до старта
		// движка): реконструируем из БД. Активная без since — консервативно в паузу.
		rt = &runtime{
			userID:      userID,
			grade:       m.Grade,
			stack:       m.Stack,
			track:       track,
			stages:      m.Stages, // issue #7: программа стадий — из строки сессии
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

// reconstructFromStore — реконструкция runtime из БД без WS-соединения
// (spec-14/A4). После рестарта сервиса recovery переводит активные сессии в
// паузу в БД, но не создаёт runtime — REST pause/resume/finish для таких
// сессий возвращали ErrNotAttached (409) навечно. Терминальные статусы
// не реконструируем (nil → прежняя ошибка «не найдена в движке»).
func (e *Engine) reconstructFromStore(id int64) *runtime {
	ctx := context.Background()
	m, err := e.store.Get(ctx, id)
	if err != nil || IsTerminalStatus(m.Status) {
		return nil
	}
	track := e.resolveTrack(m.TrackID) // issue #1: роль терялась при реконструкции
	e.mu.Lock()
	if rt, ok := e.rts[id]; ok { // уже реконструирован (гонка вызовов)
		e.mu.Unlock()
		return rt
	}
	rt := &runtime{
		userID:      m.UserID,
		grade:       m.Grade,
		stack:       m.Stack,
		track:       track,
		stages:      m.Stages, // issue #7: программа стадий — из строки сессии
		limitS:      m.DurationLimitS,
		stage:       m.Stage,
		status:      m.Status,
		active:      m.ActiveSeconds,
		lastPersist: time.Now(),
		lastTimer:   time.Now().Add(-TimerBroadcast),
	}
	if m.Status == models.StatusActive {
		// Активная без живого таймера (создана до старта движка/другим
		// экземпляром): консервативно в паузу — как Attach (FR-S7).
		e.log.Warn("reconstruct: сессия active без живого таймера — ставлю в паузу", "session", id)
		if err := e.store.MarkPaused(ctx, id, m.ActiveSeconds, e.now()); err == nil {
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
	e.mu.Unlock()
	return rt
}
