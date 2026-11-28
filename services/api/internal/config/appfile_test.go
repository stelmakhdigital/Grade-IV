package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Единый файл настроек: старшинство env > файл > дефолт и применение всего,
// что в нём есть (ADR-015).

func writeApp(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.json")
	if err := WriteAppFile(path, AppFile{}); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestAppFile_перебивает_дефолты(t *testing.T) {
	path := writeApp(t, `{"llm":{"base_url":"http://llm:9/v1","model":"Qwen3.8","enable_thinking":true},
"voice_url":"http://voice:9100","sandbox_url":"http://sandbox:9200",
"timing":{"silence_nudge_s":30,"vad_end_silence_ms":700,"vad_rms_threshold":120,
"vad_prestt_silence_ms":300,"session_pause_timeout_s":900,"session_limit_s":1800}}`)
	for _, k := range []string{"LLM_BASE_URL", "LLM_MODEL", "LLM_ENABLE_THINKING", "VOICE_URL", "SANDBOX_URL",
		"SILENCE_NUDGE_S", "VAD_END_SILENCE_MS", "VAD_RMS_THRESHOLD", "VAD_PRESTT_SILENCE_MS",
		"SESSION_PAUSE_TIMEOUT_S", "SESSION_LIMIT_S"} {
		t.Setenv(k, "")
	}
	t.Setenv("APP_CONFIG_FILE", path)

	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.LLMBaseURL != "http://llm:9/v1" || c.LLMModel != "Qwen3.8" || !c.LLMEnableThinking {
		t.Fatalf("llm: %+v", c)
	}
	if c.VoiceURL != "http://voice:9100" || c.SandboxURL != "http://sandbox:9200" {
		t.Fatalf("адреса: %+v", c)
	}
	if c.SilenceNudgeS != 30 || c.VADEndSilenceMS != 700 || c.VADRMSThreshold != 120 ||
		c.VADPreSilenceMS != 300 || c.PauseTimeoutS != 900 || c.SessionLimitS != 1800 {
		t.Fatalf("timing: %+v", c)
	}
}

func TestAppFile_env_старше_файла(t *testing.T) {
	path := writeApp(t, `{"voice_url":"http://из-файла","timing":{"silence_nudge_s":10}}`)
	t.Setenv("APP_CONFIG_FILE", path)
	t.Setenv("VOICE_URL", "http://из-env")
	t.Setenv("SILENCE_NUDGE_S", "45")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.VoiceURL != "http://из-env" || c.SilenceNudgeS != 45 {
		t.Fatalf("env проиграл файлу: %q %d", c.VoiceURL, c.SilenceNudgeS)
	}
}

func TestAppFile_битый_файл_ошибкой(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bit.json")
	if err := os.WriteFile(path, []byte("{не json"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_CONFIG_FILE", path)
	if _, err := Load(); err == nil {
		t.Fatal("битый файл должен ронять Load, а не молча игнорировать настройки")
	}
}

func TestAppFile_историческая_переменная(t *testing.T) {
	path := writeApp(t, `{"voice_url":"http://из-llm-config-file"}`)
	t.Setenv("LLM_CONFIG_FILE", path)
	t.Setenv("APP_CONFIG_FILE", "")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.VoiceURL != "http://из-llm-config-file" {
		t.Fatalf("LLM_CONFIG_FILE не работает как путь файла настроек: %q", c.VoiceURL)
	}
}
