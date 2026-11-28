// Файл переопределения LLM: config/llm.json (env LLM_CONFIG_FILE — путь).
//
// Старшинство: переменная окружения > файл > дефолт кода. env выигрывает
// намеренно — docker-compose и run-all.sh объявляют модель в составе деплоя, и
// admin-правка в файле не должна тихо переписывать прод (фидбек 2026-11-28).
//
// Ключ LLM_API_KEY в файл сознательно не едет: секрет в файле настроек, который
// отдаётся через API и попадает в бэкапы, — новая поверхность утечки. В UI
// поэтому только факт «ключ задан».
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// LLMFile — содержимое config/llm.json (всё опционально).
type LLMFile struct {
	BaseURL        string `json:"base_url,omitempty"`
	Model          string `json:"model,omitempty"`
	EnableThinking *bool  `json:"enable_thinking,omitempty"`
}

// LLMConfigPath — путь к файлу (LLM_CONFIG_FILE, иначе config/llm.json).
func LLMConfigPath() string {
	if p := os.Getenv("LLM_CONFIG_FILE"); p != "" {
		return p
	}
	return filepath.Join("config", "llm.json")
}

// ReadLLMFile читает файл; нет файла — пустое значение и nil (файла может и не быть).
func ReadLLMFile(path string) (LLMFile, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return LLMFile{}, nil
	}
	if err != nil {
		return LLMFile{}, err
	}
	var v LLMFile
	if err := json.Unmarshal(raw, &v); err != nil {
		return LLMFile{}, err
	}
	return v, nil
}

// WriteLLMFile атомично пишет файл (admin-правка применяется на старте).
func WriteLLMFile(path string, v LLMFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// applyLLMFile дописывает в Config то, что не задано окружением.
func applyLLMFile(c *Config) error {
	v, err := ReadLLMFile(LLMConfigPath())
	if err != nil {
		return err
	}
	if os.Getenv("LLM_BASE_URL") == "" && v.BaseURL != "" {
		c.LLMBaseURL = v.BaseURL
	}
	if os.Getenv("LLM_MODEL") == "" && v.Model != "" {
		c.LLMModel = v.Model
	}
	if os.Getenv("LLM_ENABLE_THINKING") == "" && v.EnableThinking != nil {
		c.LLMEnableThinking = *v.EnableThinking
	}
	return nil
}
