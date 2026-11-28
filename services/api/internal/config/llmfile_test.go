package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Файл переопределения LLM: старшинство env > файл > дефолт (фидбек 2026-11-28).

func TestLLMFile_перебивает_дефолт(t *testing.T) {
	path := filepath.Join(t.TempDir(), "llm.json")
	if err := WriteLLMFile(path, LLMFile{BaseURL: "http://llm:8300/v1", Model: "Qwen3.8", EnableThinking: ptr(true)}); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("LLM_CONFIG_FILE", path)
	t.Setenv("LLM_BASE_URL", "")
	t.Setenv("LLM_MODEL", "")
	t.Setenv("LLM_ENABLE_THINKING", "")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.LLMBaseURL != "http://llm:8300/v1" || c.LLMModel != "Qwen3.8" || !c.LLMEnableThinking {
		t.Fatalf("файл не применён: %+v", c)
	}
}

func TestLLMFile_env_старше_файла(t *testing.T) {
	path := filepath.Join(t.TempDir(), "llm.json")
	if err := WriteLLMFile(path, LLMFile{BaseURL: "http://из-файла/v1", Model: "из-файла"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("LLM_CONFIG_FILE", path)
	t.Setenv("LLM_BASE_URL", "http://из-env/v1")
	t.Setenv("LLM_MODEL", "из-env")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Иначе admin-правка в файле тихо переписывала бы прод, где модель задана в
	// составе деплоя (docker-compose, run-all.sh).
	if c.LLMBaseURL != "http://из-env/v1" || c.LLMModel != "из-env" {
		t.Fatalf("env проиграл файлу: %q %q", c.LLMBaseURL, c.LLMModel)
	}
}

func TestLLMFile_нет_файла_и_битый_файл(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "нет.json")
	t.Setenv("LLM_CONFIG_FILE", missing)
	if _, err := Load(); err != nil {
		t.Fatalf("отсутствующий файл не должен ронять Load: %v", err)
	}
	v, err := ReadLLMFile(missing)
	if err != nil || v.BaseURL != "" {
		t.Fatalf("чтение отсутствующего файла: %+v %v", v, err)
	}

	bad := filepath.Join(t.TempDir(), "bit.json")
	if err := os.WriteFile(bad, []byte("{не json"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LLM_CONFIG_FILE", bad)
	if _, err := Load(); err == nil {
		t.Fatal("битый файл должен быть ошибкой Load, а не молчаливым «настройки не применились»")
	}
}

func ptr(b bool) *bool { return &b }
