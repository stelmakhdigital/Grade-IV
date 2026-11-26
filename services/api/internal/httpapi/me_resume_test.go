package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/config"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/llm"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// docxFixture — минимальный docx: zip с word/document.xml (тот же путь, что
// разбирает internal/resume).
func docxFixture(t *testing.T, text string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatalf("zip create: %v", err)
	}
	w.Write([]byte(`<?xml version="1.0"?><w:document><w:body><w:p><w:r><w:t>` + text + `</w:t></w:r></w:p></w:body></w:document>`))
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

// putResume — PUT /me/resume (multipart). file == "" → только описание.
func putResume(t *testing.T, url, token, about, fileName string, file []byte) (int, map[string]any) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if about != "" {
		if err := mw.WriteField("about", about); err != nil {
			t.Fatalf("field: %v", err)
		}
	}
	if fileName != "" {
		part, err := mw.CreateFormFile("resume", fileName)
		if err != nil {
			t.Fatalf("form file: %v", err)
		}
		part.Write(file)
	}
	mw.Close()
	req, err := http.NewRequest("PUT", url, &body)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return res.StatusCode, out
}

// newResumeEnv — сервер с resumes-каталогом во временной папке и ответом ИИ на подбор роли.
func newResumeEnv(t *testing.T, roleAnswer string) (*httptest.Server, string) {
	t.Helper()
	ts, tok, _ := newResumeEnvRoles(t, roleAnswer)
	return ts, tok
}

// newResumeEnvRoles — то же + путь к файлу ролей: ROLE_TRACKS_FILE указывает во
// временную папку (тесты не пишут в config/roles.json рабочего дерева), и тест
// может сравнить содержимое файла до и после.
func newResumeEnvRoles(t *testing.T, roleAnswer string) (*httptest.Server, string, string) {
	t.Helper()
	dir := t.TempDir()
	rolesFile := filepath.Join(dir, "roles.json")
	t.Setenv("ROLE_TRACKS_FILE", rolesFile)
	cfg := &config.Config{
		Addr: ":0", DatabaseURL: "sqlite://:memory:", JWTSecret: "test-secret",
		JWTExpiryHours: 1, ResumeDir: dir + "/resumes",
		// Роли — дефолтный набор (backend/sysadmin/pm) в temp-файле.
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
	if roleAnswer != "" {
		mock.SetResponder(func(req llm.Request) (string, error) {
			for _, m := range req.Messages {
				if m.Role == llm.RoleSystem && strings.Contains(m.Content, "Выбери роль собеседования") {
					return roleAnswer, nil
				}
			}
			return "ok", nil
		})
	}
	srv := NewWithLLM(cfg, database, dialect, slog.New(slog.DiscardHandler), mock)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	_, reg := doJSON(t, "POST", ts.URL+"/api/v1/auth/register",
		map[string]any{"email": "cand@example.com", "password": "Test12345"}, nil)
	tok, _ := reg["token"].(string)
	if tok == "" {
		t.Fatal("нет токена кандидата")
	}
	return ts, tok, rolesFile
}

// Резюме: загрузка файла + описания, чтение, сброс.
func TestResumeUpload(t *testing.T) {
	ts, tok := newResumeEnv(t, "")
	auth := map[string]string{"Authorization": "Bearer " + tok}

	code, got := putResume(t, ts.URL+"/api/v1/me/resume", tok,
		"Go 5 лет, ищу middle+", "resume.docx", docxFixture(t, "Иван Петров, Go, 5 лет"))
	if code != http.StatusOK {
		t.Fatalf("put resume: %d %v", code, got)
	}
	if got["about"] != "Go 5 лет, ищу middle+" {
		t.Fatalf("about: %v", got["about"])
	}
	if got["file_name"] != "resume.docx" {
		t.Fatalf("file_name: %v", got["file_name"])
	}
	if n, _ := got["text_chars"].(float64); n == 0 {
		t.Fatalf("текст файла не извлечён: %v", got)
	}

	code, got = doJSON(t, "GET", ts.URL+"/api/v1/me/resume", nil, auth)
	if code != http.StatusOK || got["file_name"] != "resume.docx" {
		t.Fatalf("get resume: %d %v", code, got)
	}

	// Формат вне списка → 400.
	code, _ = putResume(t, ts.URL+"/api/v1/me/resume", tok, "", "virus.exe", []byte("MZ..."))
	if code != http.StatusBadRequest {
		t.Fatalf("unsupported format: %d, want 400", code)
	}

	// Сброс.
	req, _ := http.NewRequest("DELETE", ts.URL+"/api/v1/me/resume", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("delete resume: %d", res.StatusCode)
	}
	_, got = doJSON(t, "GET", ts.URL+"/api/v1/me/resume", nil, auth)
	if got["about"] != "" || got["file_name"] != "" {
		t.Fatalf("resume не сброшен: %v", got)
	}
}

// Старт без грейда и роли: ИИ выбрал роль по резюме, цель — на шаг выше базы.
func TestSessionCreateFromResume(t *testing.T) {
	ts, tok := newResumeEnv(t, `{"track_id":"sysadmin","grade":"middle","reason":"5 лет эксплуатации сетей"}`)
	if code, _ := putResume(t, ts.URL+"/api/v1/me/resume", tok,
		"Сисадмин, 5 лет, сети и бэкапы", "r.docx", docxFixture(t, "Опыт 5 лет")); code != http.StatusOK {
		t.Fatalf("put resume: %d", code)
	}

	code, m := doJSON(t, "POST", ts.URL+"/api/v1/sessions", map[string]any{},
		map[string]string{"Authorization": "Bearer " + tok})
	if code != http.StatusCreated {
		t.Fatalf("create: %d %v", code, m)
	}
	if m["track_id"] != "sysadmin" {
		t.Fatalf("track_id = %v, want sysadmin", m["track_id"])
	}
	if m["base_grade"] != "middle" {
		t.Fatalf("base_grade = %v, want middle", m["base_grade"])
	}
	sid, _ := m["id"].(float64)
	_, s := doJSON(t, "GET", ts.URL+"/api/v1/sessions/"+strconv.FormatInt(int64(sid), 10), nil,
		map[string]string{"Authorization": "Bearer " + tok})
	// Цель — на шаг выше базы: middle по резюме → собес как senior.
	if s["grade"] != "senior" {
		t.Fatalf("grade = %v, want senior (цель на шаг выше базы)", s["grade"])
	}
	if s["base_grade"] != "middle" {
		t.Fatalf("base_grade в DTO = %v", s["base_grade"])
	}
}

// Без резюме (ИИ не спрашивается) и при нелепом ответе ИИ — откат на backend/middle.
func TestSessionCreateFallback(t *testing.T) {
	ts, tok := newResumeEnv(t, "не json вообще")
	code, m := doJSON(t, "POST", ts.URL+"/api/v1/sessions", map[string]any{},
		map[string]string{"Authorization": "Bearer " + tok})
	if code != http.StatusCreated {
		t.Fatalf("create без резюме: %d %v", code, m)
	}
	if m["track_id"] != models.DefaultTrackID {
		t.Fatalf("track_id = %v, want %s", m["track_id"], models.DefaultTrackID)
	}
	// Резюме нет → база не определялась, грейд берётся дефолтный.
	_, s := doJSON(t, "GET", ts.URL+"/api/v1/sessions/"+strconv.FormatInt(int64(m["id"].(float64)), 10), nil,
		map[string]string{"Authorization": "Bearer " + tok})
	if s["grade"] != "middle" || s["base_grade"] != nil {
		t.Fatalf("ожидались middle без base_grade, got %v / %v", s["grade"], s["base_grade"])
	}
}

// Явный грейд в запросе — старый путь без ИИ (смоук, API-клиенты, тесты).
func TestSessionCreateExplicitGradeSkipsInference(t *testing.T) {
	ts, tok := newResumeEnv(t, "")
	code, m := doJSON(t, "POST", ts.URL+"/api/v1/sessions",
		map[string]any{"grade": "junior", "stack": "go"},
		map[string]string{"Authorization": "Bearer " + tok})
	if code != http.StatusCreated {
		t.Fatalf("create: %d %v", code, m)
	}
	if m["base_grade"] != "" {
		t.Fatalf("base_grade при явном грейде должен быть пустым, got %v", m["base_grade"])
	}
}

// issue #12: ИИ штатно даёт разные track_id и new_track.id. Запись по второму
// ключу затирала существующую роль (вплоть до backend) вплоть до рестарта.
func TestResumeRoleGenerationCannotOverwrite(t *testing.T) {
	answer := `{"track_id":"site_reliability_engineer","grade":"middle","reason":"надёжность и мониторинг",` +
		`"title":"Инженер надёжности","stacks":["go"],"new_track":{"id":"sysadmin",` +
		`"title":"Инженер надёжности (подменён)","description":"затирание дефолтной роли",` +
		`"grades":["junior"],"board":{"title":"Чужая доска","review_prompt":"…",` +
		`"groups":[{"title":"Группа","names":["Блок"]}]},` +
		`"criteria":[{"name":"Мониторинг","weight":0.85},` +
		`{"name":"Коммуникация (ясность, структура)","weight":0.15}]}}`
	ts, tok, rolesFile := newResumeEnvRoles(t, answer)
	if code, _ := putResume(t, ts.URL+"/api/v1/me/resume", tok,
		"SRE, 4 года, мониторинг и дежурства", "r.docx", docxFixture(t, "Опыт 4 года")); code != http.StatusOK {
		t.Fatalf("put resume: %d", code)
	}
	before, err := os.ReadFile(rolesFile)
	if err != nil {
		t.Fatalf("read roles: %v", err)
	}

	code, m := doJSON(t, "POST", ts.URL+"/api/v1/sessions", map[string]any{},
		map[string]string{"Authorization": "Bearer " + tok})
	if code != http.StatusCreated {
		t.Fatalf("create: %d %v", code, m)
	}
	if m["track_id"] != models.DefaultTrackID {
		t.Fatalf("track_id = %v, want %s (фолбэк, а не новая роль)", m["track_id"], models.DefaultTrackID)
	}

	// Файл ролей не перезаписан, sysadmin — дефолтный.
	after, err := os.ReadFile(rolesFile)
	if err != nil {
		t.Fatalf("read roles: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("файл ролей перезаписан:\n%s", after)
	}
	_, raw := doJSONRaw(t, "GET", ts.URL+"/api/v1/tracks", nil,
		map[string]string{"Authorization": "Bearer " + tok})
	if !strings.Contains(raw, "Системный администратор") || strings.Contains(raw, "подменён") {
		t.Fatalf("sysadmin изменён: %s", raw)
	}
}

// Новая роль по-прежнему создаётся, когда track_id и new_track.id совпадают.
func TestResumeRoleGenerationCreatesNewRole(t *testing.T) {
	answer := `{"track_id":"data-platform-engineer","grade":"middle","reason":"хранилища и пайплайны",` +
		`"title":"Инженер data-платформ","stacks":["python"],"new_track":{` +
		`"id":"data-platform-engineer","title":"Инженер data-платформ","description":"Строит хранилища и пайплайны данных.",` +
		`"practice":{"runtime":"python"},"grades":["middle","senior"],` +
		`"board":{"title":"Архитектура хранилища","review_prompt":"Оцени слои данных и потоки.",` +
		`"groups":[{"title":"Источники","names":["БД","Очередь","Файл"]}]},` +
		`"criteria":[{"name":"Моделирование данных","weight":0.55},` +
		`{"name":"Коммуникация (ясность, структура)","weight":0.45}]}}`
	ts, tok, _ := newResumeEnvRoles(t, answer)
	if code, _ := putResume(t, ts.URL+"/api/v1/me/resume", tok,
		"Хранилища, ETL, 4 года", "r.docx", docxFixture(t, "Опыт 4 года")); code != http.StatusOK {
		t.Fatalf("put resume: %d", code)
	}
	// Первый зарегистрированный — админ (ensureFirstAdmin).
	auth := map[string]string{"Authorization": "Bearer " + tok}

	code, m := doJSON(t, "POST", ts.URL+"/api/v1/sessions", map[string]any{}, auth)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %v", code, m)
	}
	if m["track_id"] != "data-platform-engineer" {
		t.Fatalf("track_id = %v, want data-platform-engineer", m["track_id"])
	}

	code, raw := doJSONRaw(t, "GET", ts.URL+"/api/v1/admin/tracks", nil, auth)
	if code != http.StatusOK || !strings.Contains(raw, `"data-platform-engineer"`) {
		t.Fatalf("новая роль не в admin-списке: %d %s", code, raw)
	}
}

func TestTargetGrade(t *testing.T) {
	all := []models.Grade{"junior", "middle", "senior", "staff"}
	pm := []models.Grade{"junior", "middle", "senior"}
	cases := []struct {
		base    models.Grade
		allowed []models.Grade
		want    models.Grade
	}{
		{"middle", all, "senior"},
		{"senior", all, "staff"},
		{"staff", all, "staff"},   // потолок — staff
		{"senior", pm, "senior"},  // роли без staff не «прокачивают» вверх
		{"", all, "middle"},       // база неизвестна — middle
		{"junior", nil, "middle"}, // нет грейдов роли — middle
	}
	for _, c := range cases {
		if got := targetGrade(c.base, c.allowed); got != c.want {
			t.Errorf("targetGrade(%q, %v) = %q, want %q", c.base, c.allowed, got, c.want)
		}
	}
}
