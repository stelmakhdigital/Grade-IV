package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func authHeader(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

// TestWhiteboardPut — PUT /whiteboard: сохранение + структура + событие whiteboard_save.
func TestWhiteboardPut(t *testing.T) {
	e := newInterviewEnv(t)

	state := json.RawMessage(`{"elements":[],"appState":{}}`)
	code, m := doJSON(t, "PUT",
		fmt.Sprintf("%s/api/v1/sessions/%d/whiteboard", e.ts.URL, e.session),
		map[string]any{
			"state":     state,
			"structure": map[string]any{"blocks": []string{"Client", "SQL-БД"}, "links": 1},
		}, authHeader(e.token))
	if code != http.StatusOK {
		t.Fatalf("PUT: %d %v", code, m)
	}
	if saved, _ := m["saved"].(bool); !saved {
		t.Fatalf("saved: %v", m)
	}

	// Сохранено в БД (state и blocks).
	_, blocks, _, err := e.srv.whiteboards.Get(context.Background(), e.session)
	if err != nil {
		t.Fatalf("whiteboards.Get: %v", err)
	}
	var st map[string]any
	if err := json.Unmarshal(blocks, &st); err != nil {
		t.Fatalf("blocks JSON: %v", err)
	}
	blocksList, _ := st["blocks"].([]any)
	if len(blocksList) != 2 {
		t.Fatalf("blocks: %v", blocks)
	}

	// Событие whiteboard_save в истории (эндпоинт /events — массив).
	req, _ := http.NewRequest("GET",
		fmt.Sprintf("%s/api/v1/sessions/%d/events", e.ts.URL, e.session), nil)
	req.Header.Set("Authorization", "Bearer "+e.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("events: %d", resp.StatusCode)
	}
	var s []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		t.Fatalf("events decode: %v", err)
	}
	var hasSave bool
	for _, ev := range s {
		if ev["kind"] == "whiteboard_save" {
			hasSave = true
		}
	}
	if !hasSave {
		t.Fatalf("нет whiteboard_save: %v", s)
	}
}

// TestWhiteboardPutErrors — 400 (пустое state), 404 (чужая сессия).
func TestWhiteboardPutErrors(t *testing.T) {
	e := newInterviewEnv(t)

	code, _ := doJSON(t, "PUT",
		fmt.Sprintf("%s/api/v1/sessions/%d/whiteboard", e.ts.URL, e.session),
		map[string]any{"structure": map[string]any{}}, authHeader(e.token))
	if code != http.StatusBadRequest {
		t.Fatalf("пустое state: %d", code)
	}

	// Чужой пользователь — 404 (второй аккаунт в том же окружении).
	var m map[string]any
	code, m = doJSON(t, "POST", e.ts.URL+"/api/v1/auth/register",
		map[string]string{"email": "other@example.com", "password": "password1"}, nil)
	if code != http.StatusCreated {
		t.Fatalf("register other: %d", code)
	}
	otherToken, _ := m["token"].(string)
	code, _ = doJSON(t, "PUT",
		fmt.Sprintf("%s/api/v1/sessions/%d/whiteboard", e.ts.URL, e.session),
		map[string]any{"state": json.RawMessage(`{"elements":[]}`)}, authHeader(otherToken))
	if code != http.StatusNotFound {
		t.Fatalf("чужая сессия: %d", code)
	}
}

// TestWhiteboardPutPNG (ADR-004, vision-оценка): PNG (base64) в PUT сохраняется
// в whiteboards, и оценка ИИ получает data-URL картинки (mock LLM — Images в
// user-сообщении).
func TestWhiteboardPutPNG(t *testing.T) {
	e := newInterviewEnv(t)

	// Минимальный валидный PNG 1×1 (67 байт, base64).
	pngB64 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="
	code, m := doJSON(t, "PUT",
		fmt.Sprintf("%s/api/v1/sessions/%d/whiteboard", e.ts.URL, e.session),
		map[string]any{
			"state":     json.RawMessage(`{"elements":[],"appState":{}}`),
			"png":       pngB64,
			"structure": map[string]any{"blocks": []string{"Client"}, "links": 0},
		}, authHeader(e.token))
	if code != http.StatusOK {
		t.Fatalf("PUT png: %d %v", code, m)
	}

	// PNG сохранён в whiteboards.
	_, _, pngPath, err := e.srv.whiteboards.Get(context.Background(), e.session)
	if err != nil {
		t.Fatalf("whiteboards.Get: %v", err)
	}
	if len(pngPath) == 0 {
		t.Fatalf("png не сохранён")
	}

	// ИИ-оценка (fire-and-forget): ждём, пока mock LLM получит запрос с data-URL
	// картинки (poll по Calls).
	deadline := time.Now().Add(5 * time.Second)
	for {
		found := false
		for _, call := range e.mock.Calls() {
			for _, msg := range call.Messages {
				if len(msg.Images) > 0 && len(msg.Images[0]) > len("data:image/png;base64,") &&
					strings.HasPrefix(msg.Images[0], "data:image/png;base64,") {
					found = true
				}
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("mock LLM не получил PNG-картинку в user-сообщении (vision-поток не сработал)")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestWhiteboardPutPNGInvalidBase64 — 400 на некорректном base64.
func TestWhiteboardPutPNGInvalidBase64(t *testing.T) {
	e := newInterviewEnv(t)
	code, _ := doJSON(t, "PUT",
		fmt.Sprintf("%s/api/v1/sessions/%d/whiteboard", e.ts.URL, e.session),
		map[string]any{
			"state": json.RawMessage(`{"elements":[]}`),
			"png":   "не-base64-!!!",
		}, authHeader(e.token))
	if code != http.StatusBadRequest {
		t.Fatalf("invalid base64: %d", code)
	}
}

// lastEvent — последнее событие указанного kind из истории сессии (через /events).
func lastEvent(t *testing.T, e *interviewEnv, kind string) (map[string]any, bool) {
	t.Helper()
	code, body := doRaw(t, "GET",
		fmt.Sprintf("%s/api/v1/sessions/%d/events", e.ts.URL, e.session), nil, nil, e.token)
	if code != http.StatusOK {
		t.Fatalf("events: %d", code)
	}
	var events []map[string]any
	if err := json.Unmarshal(body, &events); err != nil {
		t.Fatalf("events decode: %v (%s)", err, body)
	}
	var out map[string]any
	for _, ev := range events {
		if ev["kind"] == kind {
			out = ev
		}
	}
	return out, out != nil
}

// TestDesignCheckEvent — spec-24: обязательные артефакты кейса сверяются с
// блоками схемы при сдаче доски и уходят в отдельное событие design_check.
// Отчёт пересчитывает то же по whiteboard_save, но события — первоисточник:
// если в роли поменяется формулировка кейса или required, аудит должен остаться
// прежним. Для кейса без required события нет.
func TestDesignCheckEvent(t *testing.T) {
	e := newInterviewEnv(t)
	code, m := doJSON(t, "POST", e.ts.URL+"/api/v1/sessions",
		map[string]any{"grade": "middle", "track_id": "sysadmin"}, authHeader(e.token))
	if code != http.StatusCreated {
		t.Fatalf("сессия sysadmin: %d %v", code, m)
	}
	sid, _ := m["id"].(float64)
	if sid == 0 {
		t.Fatal("сессия sysadmin: пустой id")
	}
	e.session = int64(sid)

	code, _ = doJSON(t, "PUT", fmt.Sprintf("%s/api/v1/sessions/%d/whiteboard", e.ts.URL, e.session),
		map[string]any{
			"state":     json.RawMessage(`{"elements":[]}`),
			"structure": map[string]any{"blocks": []string{"Сегмент LAN", "Файрвол"}, "links": 2},
		}, authHeader(e.token))
	if code != http.StatusOK {
		t.Fatalf("PUT: %d", code)
	}

	ev, ok := lastEvent(t, e, "design_check")
	if !ok {
		t.Fatal("нет design_check для кейса с обязательными артефактами")
	}
	data, _ := ev["data"].(map[string]any)
	if data == nil {
		t.Fatalf("data не объект: %v", ev["data"])
	}
	missing, _ := data["missing"].([]any)
	if len(missing) == 0 {
		t.Fatalf("missing пуст при 2 блоках из 6: %v", data)
	}
	if !strings.Contains(fmt.Sprint(missing), "VPN") {
		t.Fatalf("в missing нет VPN: %v", missing)
	}
	if req, _ := data["required"].(float64); req < 3 {
		t.Fatalf("required = %v, ожидалось >= 3", data["required"])
	}

	// Кейс без обязательных артефактов (backend) — событий не появляется.
	back := newInterviewEnv(t)
	code, _ = doJSON(t, "PUT", fmt.Sprintf("%s/api/v1/sessions/%d/whiteboard", back.ts.URL, back.session),
		map[string]any{
			"state":     json.RawMessage(`{"elements":[]}`),
			"structure": map[string]any{"blocks": []string{"API", "БД"}, "links": 1},
		}, authHeader(back.token))
	if code != http.StatusOK {
		t.Fatalf("PUT backend: %d", code)
	}
	if ev, ok := lastEvent(t, back, "design_check"); ok {
		t.Fatalf("design_check появился для кейса без required: %v", ev)
	}
}

// TestWhiteboardGetEmpty: на первом входе в стадию схема ещё не сохранялась.
// Раньше это был 404 — клиент его глотал, но в консоли он выглядел как упавший
// запрос на каждой новой доске. Пустое состояние — штатный случай: 200, state=null.
func TestWhiteboardGetEmpty(t *testing.T) {
	e := newInterviewEnv(t)
	code, m := doJSON(t, "GET",
		fmt.Sprintf("%s/api/v1/sessions/%d/whiteboard", e.ts.URL, e.session), nil, authHeader(e.token))
	if code != http.StatusOK {
		t.Fatalf("GET пустого холста: %d %v", code, m)
	}
	if v, ok := m["state"]; !ok || v != nil {
		t.Errorf("state = %v (ключ есть: %v), хочу null", m["state"], ok)
	}
	st, ok := m["structure"].(map[string]any)
	if !ok {
		t.Fatalf("structure = %T, хочу объект", m["structure"])
	}
	if blocks, isArray := st["blocks"].([]any); !isArray || len(blocks) != 0 {
		t.Errorf("blocks = %v, хочу пустой список", st["blocks"])
	}
}
