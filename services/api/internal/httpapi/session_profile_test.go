package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// TestSessionCreateWithProfile — POST /api/v1/sessions с profile_id (Итерация B):
// валидация (404 на несуществующий) и сохранение (GET сессии — profile_id).
func TestSessionCreateWithProfile(t *testing.T) {
	ts := newTestEnv(t)
	token := registerAndGetToken(t, ts)

	// Профили: пресеты сидированы при старте.
	code, raw := doRaw(t, "GET", ts.URL+"/api/v1/profiles", nil, authHeader(token), "")
	if code != http.StatusOK {
		t.Fatalf("profiles list: status = %d (body: %s)", code, raw)
	}
	var profiles []struct {
		ID         int64  `json:"id"`
		Name       string `json:"name"`
		Tone       string `json:"tone"`
		Difficulty string `json:"difficulty"`
		IsPreset   bool   `json:"is_preset"`
	}
	if err := json.Unmarshal(raw, &profiles); err != nil {
		t.Fatalf("profiles list: unmarshal: %v (%s)", err, raw)
	}
	if len(profiles) < 10 {
		t.Fatalf("пресетов = %d, want ≥ 10", len(profiles))
	}
	var strict *int64
	for i := range profiles {
		if profiles[i].Tone == "strict" && profiles[i].Difficulty == "plus" {
			strict = &profiles[i].ID
			break
		}
	}
	if strict == nil {
		t.Fatal("нет пресета strict/plus")
	}

	// Создание с profile_id → 201; GET сессии — profile_id сохранён.
	body := fmt.Sprintf(`{"grade":"middle","stack":"go","profile_id":%d}`, *strict)
	code, raw = doRaw(t, "POST", ts.URL+"/api/v1/sessions", []byte(body), nil, token)
	if code != http.StatusCreated {
		t.Fatalf("create с profile_id: status = %d (body: %s)", code, raw)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("create: unmarshal: %v", err)
	}
	id := int64(m["id"].(float64))
	code, raw = doRaw(t, "GET", fmt.Sprintf("%s/api/v1/sessions/%d", ts.URL, id), nil, authHeader(token), "")
	if code != http.StatusOK {
		t.Fatalf("get session: status = %d", code)
	}
	var sess map[string]any
	if err := json.Unmarshal(raw, &sess); err != nil {
		t.Fatalf("get session: unmarshal: %v", err)
	}
	if got := int64(sess["profile_id"].(float64)); got != *strict {
		t.Fatalf("session.profile_id = %d, want %d", got, *strict)
	}

	// Несуществующий profile_id → 404.
	code, raw = doRaw(t, "POST", ts.URL+"/api/v1/sessions",
		[]byte(`{"grade":"middle","stack":"go","profile_id":999999}`), nil, token)
	if code != http.StatusNotFound {
		t.Fatalf("create с плохим profile_id: status = %d, want 404 (body: %s)", code, raw)
	}

	// Без profile_id → 0 (default-профиль: balanced/standard).
	code, raw = doRaw(t, "POST", ts.URL+"/api/v1/sessions",
		[]byte(`{"grade":"middle","stack":"go"}`), nil, token)
	if code != http.StatusCreated {
		t.Fatalf("create без profile_id: status = %d", code)
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("create без profile_id: unmarshal: %v", err)
	}
	id = int64(m["id"].(float64))
	code, raw = doRaw(t, "GET", fmt.Sprintf("%s/api/v1/sessions/%d", ts.URL, id), nil, authHeader(token), "")
	if code != http.StatusOK {
		t.Fatalf("get session (default): status = %d", code)
	}
	if err := json.Unmarshal(raw, &sess); err != nil {
		t.Fatalf("get session (default): unmarshal: %v", err)
	}
	if got := sess["profile_id"]; got != nil && int64(got.(float64)) != 0 {
		t.Fatalf("session.profile_id = %v, want 0 (default)", got)
	}
}
