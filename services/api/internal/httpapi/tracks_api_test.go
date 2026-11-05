package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/config"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/llm"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// TestTracksListAndCreateSession — публичный список треков + создание сессии
// на некодовом треке (spec-18/Ф1).
func TestTracksListAndCreateSession(t *testing.T) {
	ts := newTestEnv(t)
	_, reg := doJSON(t, "POST", ts.URL+"/api/v1/auth/register",
		map[string]any{"email": "track-a@example.com", "password": "Test12345"}, nil)
	token, _ := reg["token"].(string)
	if token == "" {
		t.Fatal("register: пустой token")
	}
	auth := map[string]string{"Authorization": "Bearer " + token}

	// Список: ≥3 роли; sysadmin — практика shell, backend — практика go (spec-22).
	code, raw := doJSONRaw(t, "GET", ts.URL+"/api/v1/tracks", nil, auth)
	if code != 200 {
		t.Fatalf("GET /tracks: %d", code)
	}
	if !strings.Contains(raw, `"sysadmin"`) || !strings.Contains(raw, `"practice":{"runtime":"shell"}`) {
		t.Fatalf("в списке нет sysadmin с практикой shell: %s", raw)
	}

	// Сессия на sysadmin middle → ок; track_id и наследованный стек практики.
	code, m := doJSON(t, "POST", ts.URL+"/api/v1/sessions",
		map[string]any{"grade": "middle", "track_id": "sysadmin"}, auth)
	if code != 201 {
		t.Fatalf("create sysadmin session: %d %v", code, m)
	}
	if got, _ := m["track_id"].(string); got != "sysadmin" {
		t.Fatalf("track_id в ответе: %v", m["track_id"])
	}
	if got, _ := m["stack"].(string); got != "shell" {
		t.Fatalf("стек практики в сессии: %v, want shell", m["stack"])
	}

	// spec-22: клиентский стек игнорируется — роль задаёт рантайм сам.
	code, _ = doJSON(t, "POST", ts.URL+"/api/v1/sessions",
		map[string]any{"grade": "middle", "stack": "rust", "track_id": "backend"}, auth)
	if code != 201 {
		t.Fatalf("чужой стек игнорируется: %d, want 201", code)
	}

	// Неизвестный трек → 400.
	code, _ = doJSON(t, "POST", ts.URL+"/api/v1/sessions",
		map[string]any{"grade": "middle", "track_id": "no-such"}, auth)
	if code != 400 {
		t.Fatalf("неизвестный трек: %d, want 400", code)
	}

	// Грейд вне трека → 400 (staff не входит в sysadmin).
	code, _ = doJSON(t, "POST", ts.URL+"/api/v1/sessions",
		map[string]any{"grade": "staff", "track_id": "sysadmin"}, auth)
	if code != 400 {
		t.Fatalf("staff в sysadmin: %d, want 400", code)
	}
}

// TestAdminTrackCRUD — admin-CRUD треков + защита используемого трека.
func TestAdminTrackCRUD(t *testing.T) {
	ts := newTestEnv(t)
	_, reg := doJSON(t, "POST", ts.URL+"/api/v1/auth/register",
		map[string]any{"email": "track-admin@example.com", "password": "Test12345"}, nil)
	token, _ := reg["token"].(string)
	if token == "" {
		t.Fatal("register: пустой token")
	}
	auth := map[string]string{"Authorization": "Bearer " + token}
	admin := func(method, path string, body any) (int, map[string]any) {
		return doJSON(t, method, ts.URL+path, body, auth)
	}

	// Не-админ (второй пользователь) → 403.
	_, reg2 := doJSON(t, "POST", ts.URL+"/api/v1/auth/register",
		map[string]any{"email": "track-user@example.com", "password": "Test12345"}, nil)
	token2, _ := reg2["token"].(string)
	code, _ := doJSON(t, "GET", ts.URL+"/api/v1/admin/tracks", nil,
		map[string]string{"Authorization": "Bearer " + token2})
	if code != 403 {
		t.Fatalf("не-админ: %d, want 403", code)
	}

	tk := models.Track{
		ID:     "qa",
		Title:  "QA-инженер",
		Grades: []models.Grade{models.GradeJunior, models.GradeMiddle},
		Board: models.BoardConfig{
			Title:        "Схема тест-плана",
			ReviewPrompt: "Оцени полноту покрытия и глубину анализа рисков.",
			Groups:       []models.BlockGroup{{Title: "Тесты", Names: []string{"Модуль", "Сценарий"}}},
		},
		Criteria: []models.TrackCriterion{
			{Name: "Качество тест-дизайна", Weight: 0.5},
			{Name: "Коммуникация", Weight: 0.5},
		},
	}

	code, m := admin("POST", "/api/v1/admin/tracks", map[string]any{"track": tk})
	if code != 201 {
		t.Fatalf("create: %d %v", code, m)
	}
	// Дубликат → 409.
	code, _ = admin("POST", "/api/v1/admin/tracks", map[string]any{"track": tk})
	if code != 409 {
		t.Fatalf("дубликат: %d, want 409", code)
	}
	// Битая запись (веса мимо допуска) → 400. Копия с глубокой копией слайса
	// (алиасинг срезов tk.Criteria — иначе портим валидный трек).
	bad := tk
	bad.ID = "bad"
	bad.Criteria = []models.TrackCriterion{{Name: "A", Weight: 0.2}, {Name: "B", Weight: 0.2}}
	code, _ = admin("POST", "/api/v1/admin/tracks", map[string]any{"track": bad})
	if code != 400 {
		t.Fatalf("битые веса: %d, want 400", code)
	}
	// Обновление.
	tk.Title = "QA-инженер (обновлён)"
	code, mup := admin("PUT", "/api/v1/admin/tracks/qa", map[string]any{"track": tk})
	if code != 200 {
		t.Fatalf("update: %d %v", code, mup)
	}
	// Удаление используемого трека → 409.
	code, _ = doJSON(t, "POST", ts.URL+"/api/v1/sessions",
		map[string]any{"grade": "middle", "track_id": "qa"}, auth)
	if code != 201 {
		t.Fatalf("create qa session: %d", code)
	}
	code, _ = admin("DELETE", "/api/v1/admin/tracks/qa", nil)
	if code != 409 {
		t.Fatalf("delete используемого: %d, want 409", code)
	}
	// Удаление backend → 400 (дефолт совместимости).
	code, _ = admin("DELETE", "/api/v1/admin/tracks/backend", nil)
	if code != 400 {
		t.Fatalf("delete backend: %d, want 400", code)
	}
}

// doJSONRaw — как doJSON, но тело не парсится (для JSON-массивов).
func doJSONRaw(t *testing.T, method, url string, body any, headers map[string]string) (int, string) {
	t.Helper()
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return resp.StatusCode, string(raw)
}

// TestTrackGenerate — spec-22: черновик роли по описанию (ИИ). Мок возвращает
// валидный JSON-черновик; черновик НЕ сохраняется; не-админ → 403.
func TestTrackGenerate(t *testing.T) {
	cfg := &config.Config{
		Addr: ":0", DatabaseURL: "sqlite://:memory:", JWTSecret: "test-secret",
		JWTExpiryHours: 1, MinutesFreeS: 3600,
	}
	database, dialect, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.Migrate(context.Background(), database, dialect); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	mock := llm.NewMockProvider()
	mock.SetResponder(func(req llm.Request) (string, error) {
		for _, msg := range req.Messages {
			if msg.Role == llm.RoleSystem && strings.Contains(msg.Content, "конфигурируешь роль") {
				return trackMockDraft, nil
			}
		}
		return "не туда", nil
	})
	logger := slog.New(slog.DiscardHandler)
	srv := NewWithLLM(cfg, database, dialect, logger, mock)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	// Первый пользователь = админ.
	_, reg := doJSON(t, "POST", ts.URL+"/api/v1/auth/register",
		map[string]any{"email": "roles-admin@example.com", "password": "Test12345"}, nil)
	adminTok, _ := reg["token"].(string)
	admin := map[string]string{"Authorization": "Bearer " + adminTok}

	// Не-админ → 403.
	_, reg2 := doJSON(t, "POST", ts.URL+"/api/v1/auth/register",
		map[string]any{"email": "plain@example.com", "password": "Test12345"}, nil)
	plainTok, _ := reg2["token"].(string)
	plain := map[string]string{"Authorization": "Bearer " + plainTok}
	code, _ := doJSON(t, "POST", ts.URL+"/api/v1/admin/tracks/generate",
		map[string]any{"description": "Инженер по данным на Python: пайплайны и дашборды"}, plain)
	if code != http.StatusForbidden {
		t.Fatalf("не-админ: %d, want 403", code)
	}

	// Слишком короткое описание → 400.
	code, _ = doJSON(t, "POST", ts.URL+"/api/v1/admin/tracks/generate",
		map[string]any{"description": "коротко"}, admin)
	if code != http.StatusBadRequest {
		t.Fatalf("короткое описание: %d, want 400", code)
	}

	// Успех: черновик валиден, практика из ответа ИИ, критерии нормализованы.
	code, m := doJSON(t, "POST", ts.URL+"/api/v1/admin/tracks/generate",
		map[string]any{"description": "Инженер по данным на Python: пайплайны, хранилища и дашборды"}, admin)
	if code != http.StatusOK {
		t.Fatalf("generate: %d %v", code, m)
	}
	tr, _ := m["track"].(map[string]any)
	if tr == nil {
		t.Fatalf("нет track в ответе: %v", m)
	}
	pr, _ := tr["practice"].(map[string]any)
	if pr == nil || pr["runtime"] != "python" {
		t.Fatalf("практика черновика: %v", tr["practice"])
	}
	crit, _ := tr["criteria"].([]any)
	sum := 0.0
	for _, c := range crit {
		sum += c.(map[string]any)["weight"].(float64)
	}
	if sum < 0.999 || sum > 1.001 {
		t.Fatalf("сумма весов %.3f", sum)
	}

	// Черновик не сохранён: в списке треков его нет.
	code, raw := doJSONRaw(t, "GET", ts.URL+"/api/v1/admin/tracks", nil, admin)
	if code != http.StatusOK {
		t.Fatalf("admin list: %d", code)
	}
	if strings.Contains(raw, "Data-инженер") {
		t.Fatalf("черновик сохранился: %s", raw)
	}
}
