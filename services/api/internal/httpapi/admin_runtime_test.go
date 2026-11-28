package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/config"
)

// Контур настроек (фидбек 2026-11-28): панель «что подключено» и выбор моделей.

// adminAuth — токен первого зарегистрированного (он админ).
func adminAuth(t *testing.T, ts *httptest.Server, email string) map[string]string {
	t.Helper()
	code, m := doJSON(t, "POST", ts.URL+"/api/v1/auth/register",
		map[string]any{"email": email, "password": "Test12345"}, nil)
	if code != 200 && code != 201 {
		t.Fatalf("register: %d %v", code, m)
	}
	token, _ := m["token"].(string)
	if token == "" {
		t.Fatal("register: пустой token")
	}
	return map[string]string{"Authorization": "Bearer " + token}
}

// voiceStub — заглушка voice: пишет, что прилетело в PUT /config/models.
func voiceStub(t *testing.T, putStatus int) (*httptest.Server, *map[string]string) {
	t.Helper()
	got := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/config/models" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"current":  map[string]any{"stt": map[string]any{"provider": "gigaam"}},
				"writable": true,
				"applies":  "restart",
			})
		case r.URL.Path == "/api/v1/config/models" && r.Method == http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			got["token"] = r.Header.Get("X-Admin-Token")
			got["body"] = string(body)
			w.WriteHeader(putStatus)
			_, _ = w.Write([]byte(`{"saved":true,"missing_weights":{}}`))
			if putStatus != 200 {
				_, _ = w.Write([]byte(`{"detail":{"code":"validation","message":"нет такой модели"}}`))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestAdminRuntime_показывает_источники_и_недоступный_voice(t *testing.T) {
	// voice недоступен (URL в никуда) — панель всё равно 200 и говорит, ЧЕГО не видно.
	ts := newTestServer(t, func(cfg *config.Config) {
		cfg.VoiceURL = "http://127.0.0.1:1"
		cfg.LLMAPIKey = "sk-secret"
	})
	auth := adminAuth(t, ts, "rt-admin@example.com")

	code, body := doJSON(t, "GET", ts.URL+"/api/v1/admin/runtime", nil, auth)
	if code != 200 {
		t.Fatalf("runtime: %d %v", code, body)
	}
	llm, _ := body["llm"].(map[string]any)
	if llm == nil {
		t.Fatalf("нет блока llm: %v", body)
	}
	// Ключ наружу не отдаётся — только факт, что задан.
	if llm["api_key_set"] != true {
		t.Fatalf("api_key_set: %v", llm["api_key_set"])
	}
	for k := range llm {
		if strings.Contains(strings.ToLower(k), "key") && k != "api_key_set" {
			t.Fatalf("в панель уехал ключ: %v", llm)
		}
	}
	base, _ := llm["base_url"].(map[string]any)
	if base == nil || base["source"] == nil || base["env"] != "LLM_BASE_URL" {
		t.Fatalf("base_url без источника: %v", llm["base_url"])
	}
	voice, _ := body["voice"].(map[string]any)
	if voice == nil || voice["error"] == nil {
		t.Fatalf("недоступный voice должен быть виден ошибкой в блоке, а не 502: %v", body["voice"])
	}
	if body["applies"] != "restart" {
		t.Fatalf("applies: %v", body["applies"])
	}

	// Не-админ 403.
	_, reg := doJSON(t, "POST", ts.URL+"/api/v1/auth/register",
		map[string]any{"email": "rt-user@example.com", "password": "Test12345"}, nil)
	tok, _ := reg["token"].(string)
	code, _ = doJSON(t, "GET", ts.URL+"/api/v1/admin/runtime", nil,
		map[string]string{"Authorization": "Bearer " + tok})
	if code != 403 {
		t.Fatalf("не-админ: %d, want 403", code)
	}
}

func TestAdminModelsPut_llm_в_файл(t *testing.T) {
	path := filepath.Join(t.TempDir(), "llm.json")
	t.Setenv("LLM_CONFIG_FILE", path)
	ts := newTestServer(t, nil)
	auth := adminAuth(t, ts, "rt-llm@example.com")

	code, body := doJSON(t, "PUT", ts.URL+"/api/v1/admin/models", map[string]any{
		"llm": map[string]any{"base_url": "http://llm:9/v1", "model": "Qwen3.8", "enable_thinking": true},
	}, auth)
	if code != 200 {
		t.Fatalf("put: %d %v", code, body)
	}
	applied, _ := body["applied"].(map[string]any)
	llmApplied, _ := applied["llm"].(map[string]any)
	if llmApplied["saved"] != true || llmApplied["applies"] != "restart" {
		t.Fatalf("applied.llm: %v", applied)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("файл не записан: %v", err)
	}
	if !strings.Contains(string(raw), "Qwen3.8") || !strings.Contains(string(raw), "enable_thinking") {
		t.Fatalf("содержимое файла: %s", raw)
	}
	if strings.Contains(strings.ToLower(string(raw)), "api_key") {
		t.Fatalf("ключ не должен попадать в файл настроек: %s", raw)
	}

	// Гарнитура: пустое тело — 400, а не «успех без изменений».
	code, _ = doJSON(t, "PUT", ts.URL+"/api/v1/admin/models", map[string]any{}, auth)
	if code != 400 {
		t.Fatalf("пустой запрос: %d, want 400", code)
	}
}

func TestAdminModelsPut_голоса_через_voice(t *testing.T) {
	stub, got := voiceStub(t, 200)
	ts := newTestServer(t, func(cfg *config.Config) {
		cfg.VoiceURL = stub.URL
		cfg.VoiceAdminToken = "tok-admin"
	})
	auth := adminAuth(t, ts, "rt-voice@example.com")

	code, body := doJSON(t, "PUT", ts.URL+"/api/v1/admin/models", map[string]any{
		"stt": map[string]any{"provider": "gigaam", "model": "v3_e2e_ctc"},
		"tts": map[string]any{"provider": "silero", "speaker": "xenia"},
	}, auth)
	if code != 200 {
		t.Fatalf("put: %d %v", code, body)
	}
	if (*got)["token"] != "tok-admin" {
		t.Fatalf("токен voice не проброшен: %v", got)
	}
	if !strings.Contains((*got)["body"], "v3_e2e_ctc") {
		t.Fatalf("тело voice: %v", (*got)["body"])
	}
	applied, _ := body["applied"].(map[string]any)
	v, _ := applied["voice"].(map[string]any)
	if v["saved"] != true {
		t.Fatalf("applied.voice: %v", applied)
	}
}

func TestAdminModelsPut_без_токена_voice_не_пишет(t *testing.T) {
	stub, got := voiceStub(t, 200)
	ts := newTestServer(t, func(cfg *config.Config) { cfg.VoiceURL = stub.URL })
	auth := adminAuth(t, ts, "rt-notoken@example.com")

	code, body := doJSON(t, "PUT", ts.URL+"/api/v1/admin/models", map[string]any{
		"stt": map[string]any{"provider": "gigaam", "model": "v3_e2e_ctc"},
	}, auth)
	if code != 200 {
		t.Fatalf("put: %d %v", code, body)
	}
	applied, _ := body["applied"].(map[string]any)
	v, _ := applied["voice"].(map[string]any)
	if v["saved"] != false {
		t.Fatalf("без VOICE_ADMIN_TOKEN писать нельзя: %v", applied)
	}
	if (*got)["token"] != "" {
		t.Fatalf("запрос в voice всё-таки ушёл: %v", got)
	}
}

func TestAdminModelsPut_ошибка_voice_отдаётся_как_есть(t *testing.T) {
	stub, _ := voiceStub(t, 400)
	ts := newTestServer(t, func(cfg *config.Config) {
		cfg.VoiceURL = stub.URL
		cfg.VoiceAdminToken = "tok-admin"
	})
	auth := adminAuth(t, ts, "rt-bad@example.com")

	code, body := doJSON(t, "PUT", ts.URL+"/api/v1/admin/models", map[string]any{
		"stt": map[string]any{"provider": "gigaam", "model": "нет-такой"},
	}, auth)
	if code != 200 {
		t.Fatalf("put: %d %v", code, body)
	}
	applied, _ := body["applied"].(map[string]any)
	v, _ := applied["voice"].(map[string]any)
	// 400 от voice — ответ админу про его выбор, а не авария api.
	if v["status"] != float64(400) || v["saved"] != false {
		t.Fatalf("applied.voice: %v", applied)
	}
}
