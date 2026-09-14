package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
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
