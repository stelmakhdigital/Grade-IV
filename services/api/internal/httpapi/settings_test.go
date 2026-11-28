package httpapi

import (
	"fmt"

	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/config"
)

// Единый экран настроек (ADR-015): источник значения, «нужен перезапуск» и то,
// что перекрывает переменная окружения.

func settingsGet(t *testing.T, ts string, auth map[string]string) map[string]any {
	t.Helper()
	code, body := doJSON(t, "GET", ts+"/api/v1/settings", nil, auth)
	if code != 200 {
		t.Fatalf("settings: %d %v", code, body)
	}
	return body
}

func TestSettings_get_показывает_источники_и_то_что_не_правится(t *testing.T) {
	t.Setenv("APP_CONFIG_FILE", filepath.Join(t.TempDir(), "app.json"))
	t.Setenv("SILENCE_NUDGE_S", "30")
	// voice отвечает — блок собирается из его ответа плюс адрес с источником;
	// недоступный voice проверяется отдельным тестом (nil-map там паниковал).
	stub, _ := voiceStub(t, 200)
	ts := newTestServer(t, func(cfg *config.Config) { cfg.VoiceURL = stub.URL })
	auth := adminAuth(t, ts, "st-admin@example.com")
	body := settingsGet(t, ts.URL, auth)

	timing, _ := body["timing"].(map[string]any)
	if timing == nil {
		t.Fatalf("нет блока timing: %v", body)
	}
	nudge, _ := timing["silence_nudge_s"].(map[string]any)
	if nudge["source"] != "env" || nudge["env"] != "SILENCE_NUDGE_S" {
		t.Fatalf("silence_nudge_s без источника: %v", timing["silence_nudge_s"])
	}
	end, _ := timing["vad_end_silence_ms"].(map[string]any)
	if end["source"] != "default" {
		t.Fatalf("vad_end_silence_ms: %v", end)
	}

	// Установка: эффективные значения видны, секретов нет, редактировать нельзя.
	install, _ := body["install"].(map[string]any)
	if install["editable"] != false {
		t.Fatalf("install должен быть только для чтения: %v", install)
	}
	for _, k := range []string{"jwt_secret", "metrics_password", "llm_api_key"} {
		if _, ok := install[k]; ok {
			t.Fatalf("секрет уехал в настройки: %v", install)
		}
	}
	if install["jwt_secret_set"] != true {
		t.Fatalf("факт наличия секрета не показан: %v", install)
	}
	if install["database_url"] == nil || install["database_url"] == "" {
		t.Fatalf("адрес БД не показан: %v", install)
	}
	// Адрес voice — тоже настройка, и он обязан быть в ответе с источником:
	// без него поле «адрес голосового сервиса» показывает пустоту там, где
	// значение есть.
	voice, _ := body["voice"].(map[string]any)
	addr, _ := voice["address"].(map[string]any)
	if addr["env"] != "VOICE_URL" || addr["source"] == nil {
		t.Fatalf("voice.address без источника: %v", voice["address"])
	}
	if addr["value"] != stub.URL {
		t.Fatalf("voice.address не эффективное значение: %v", addr)
	}

	paths, _ := body["paths"].(map[string]any)
	if !strings.HasSuffix(fmt.Sprintf("%v", paths["app_file"]), "app.json") {
		t.Fatalf("пути: %v", paths)
	}
	if body["applies"] != "restart" {
		t.Fatalf("applies: %v", body["applies"])
	}
}
func TestSettings_put_тайминга_просит_перезапуск(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.json")
	t.Setenv("APP_CONFIG_FILE", path)
	t.Setenv("SILENCE_NUDGE_S", "")
	ts := newTestServer(t, nil)
	auth := adminAuth(t, ts, "st-timing@example.com")

	code, body := doJSON(t, "PUT", ts.URL+"/api/v1/settings", map[string]any{
		"timing":    map[string]any{"silence_nudge_s": 10, "vad_end_silence_ms": 700},
		"llm":       map[string]any{"model": "Qwen3.8"},
		"voice_url": "http://voice:9100",
	}, auth)
	if code != 200 {
		t.Fatalf("put: %d %v", code, body)
	}
	applied, _ := body["applied"].(map[string]any)
	app, _ := applied["app"].(map[string]any)
	if app["saved"] != true {
		t.Fatalf("applied.app: %v", applied)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("файл не записан: %v", err)
	}
	if !strings.Contains(string(raw), "silence_nudge_s") || !strings.Contains(string(raw), "voice_url") {
		t.Fatalf("содержимое: %s", raw)
	}
	if strings.Contains(strings.ToLower(string(raw)), "api_key") {
		t.Fatalf("ключ не должен попадать в файл: %s", raw)
	}

	// Процесс ещё читал старые значения — значит ключи в pending с needs_restart.
	pending, _ := body["pending"].([]any)
	var nudge map[string]any
	for _, p := range pending {
		m, _ := p.(map[string]any)
		if m["key"] == "timing.silence_nudge_s" {
			nudge = m
		}
	}
	if nudge == nil || nudge["status"] != "needs_restart" {
		t.Fatalf("pending без needs_restart: %v", pending)
	}

	// GET после записи: значение уже из файла (source=file), pending живёт.
	again := settingsGet(t, ts.URL, auth)
	timing, _ := again["timing"].(map[string]any)
	n2, _ := timing["silence_nudge_s"].(map[string]any)
	if n2["source"] != "file" {
		t.Fatalf("после записи источник не file: %v", timing["silence_nudge_s"])
	}
}

func TestSettings_переменная_перекрывает_файл(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.json")
	t.Setenv("APP_CONFIG_FILE", path)
	t.Setenv("SILENCE_NUDGE_S", "30") // деплой закрепил значение
	ts := newTestServer(t, nil)
	auth := adminAuth(t, ts, "st-shadow@example.com")

	_, body := doJSON(t, "PUT", ts.URL+"/api/v1/settings", map[string]any{
		"timing": map[string]any{"silence_nudge_s": 10},
	}, auth)
	pending, _ := body["pending"].([]any)
	if len(pending) != 1 {
		t.Fatalf("ожидали один ключ в pending: %v", pending)
	}
	item, _ := pending[0].(map[string]any)
	if item["status"] != "shadowed_by_env" || item["env"] != "SILENCE_NUDGE_S" {
		t.Fatalf("pending: %v", item)
	}
	if !strings.Contains(fmt.Sprintf("%v", item["note"]), "перекрывает") {
		t.Fatalf("нет объяснения: %v", item)
	}
}

func TestSettings_пустое_тело_400(t *testing.T) {
	t.Setenv("APP_CONFIG_FILE", filepath.Join(t.TempDir(), "app.json"))
	ts := newTestServer(t, nil)
	auth := adminAuth(t, ts, "st-empty@example.com")
	code, _ := doJSON(t, "PUT", ts.URL+"/api/v1/settings", map[string]any{}, auth)
	if code != 400 {
		t.Fatalf("пустой запрос: %d, want 400", code)
	}
}

func TestSettings_алиас_админки_жив(t *testing.T) {
	t.Setenv("APP_CONFIG_FILE", filepath.Join(t.TempDir(), "app.json"))
	ts := newTestServer(t, nil)
	auth := adminAuth(t, ts, "st-alias@example.com")
	code, body := doJSON(t, "GET", ts.URL+"/api/v1/admin/runtime", nil, auth)
	if code != 200 {
		t.Fatalf("алиас: %d %v", code, body)
	}
	if _, ok := body["llm"]; !ok {
		t.Fatalf("алиас не отдаёт ту же схему: %v", body)
	}
}

func TestSettings_лимит_сессии_по_грейду_не_минус_один(t *testing.T) {
	t.Setenv("APP_CONFIG_FILE", filepath.Join(t.TempDir(), "app.json"))
	t.Setenv("SESSION_LIMIT_S", "")
	ts := newTestServer(t, nil) // в коде «не задано» = -1
	auth := adminAuth(t, ts, "st-limit@example.com")
	body := settingsGet(t, ts.URL, auth)
	timing, _ := body["timing"].(map[string]any)
	limit, _ := timing["session_limit_s"].(map[string]any)
	// -1 в поле означало бы «интервью длится минус секунду»; для человека это 0
	// и подпись «по грейду».
	if limit["value"] != float64(0) {
		t.Fatalf("session_limit_s: %v, want 0", limit["value"])
	}
}
