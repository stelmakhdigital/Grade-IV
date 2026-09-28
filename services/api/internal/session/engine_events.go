package session

import (
	"context"
	"encoding/json"
	"time"

	"nhooyr.io/websocket"
)

// addEvent — событие сессии (data сериализуется в JSON).
func (e *Engine) addEvent(ctx context.Context, sessionID int64, kind string, data map[string]any) (int, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return 0, err
	}
	return e.store.AddEvent(ctx, sessionID, kind, raw)
}

// SendTo — публичная отправка JSON-сообщения клиенту сессии (безопасно: один писатель).
func (e *Engine) SendTo(id int64, v any) { e.sendJSON(id, v) }

// SendBinary — отправка бинарного кадра (PCM аудио ИИ, ADR-001; один писатель).
func (e *Engine) SendBinary(id int64, data []byte) {
	rt := e.rt(id)
	if rt == nil {
		return
	}
	rt.connMu.Lock()
	defer rt.connMu.Unlock()
	if rt.conn == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rt.conn.Write(ctx, websocket.MessageBinary, data); err != nil {
		e.log.Warn("ws: ошибка записи binary", "session", id, "err", err)
	}
}

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
