// Единый файл настроек приложения: config/app.json (env LLM_CONFIG_FILE или
// APP_CONFIG_FILE — путь). Пишет его экран «Настройки», читает api при старте.
//
// Старшинство: переменная окружения > файл > дефолт кода. env выигрывает
// намеренно: docker-compose и run-all.sh объявляют состав деплоя, и правка в
// файле не должна тихо переписывать прод.
//
// Чего в файле нет сознательно: секреты (LLM_API_KEY, JWT_SECRET, пароли БД),
// адрес БД, порты, лимиты контейнеров. Ошибка в них лишает приложения без
// «отменить», а настраиваются они всё равно снаружи.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// AppFile — содержимое config/app.json. Всё опционально: отсутствие поля значит
// «не переопределяем».
type AppFile struct {
	LLM struct {
		BaseURL        string `json:"base_url,omitempty"`
		Model          string `json:"model,omitempty"`
		EnableThinking *bool  `json:"enable_thinking,omitempty"`
	} `json:"llm"`
	// VoiceURL / SandboxURL — куда ходить за голосом и песочницей. Нужны, когда
	// локальный запуск поднимает сервисы сам или, наоборот, зовёт удалённые.
	VoiceURL   string `json:"voice_url,omitempty"`
	SandboxURL string `json:"sandbox_url,omitempty"`
	// Timing — качество голосового хода и рамки сессии. Пороги VAD правятся по
	// замерам, но обычному человеку они нужнее, чем админу с ssh.
	Timing struct {
		SilenceNudgeS   *int `json:"silence_nudge_s,omitempty"`
		VADEndSilenceMS *int `json:"vad_end_silence_ms,omitempty"`
		VADRMS          *int `json:"vad_rms_threshold,omitempty"`
		VADPreSilenceMS *int `json:"vad_prestt_silence_ms,omitempty"`
		PauseTimeoutS   *int `json:"session_pause_timeout_s,omitempty"`
		SessionLimitS   *int `json:"session_limit_s,omitempty"`
	} `json:"timing"`
}

// AppConfigPath — путь к файлу (APP_CONFIG_FILE, затем LLM_CONFIG_FILE, иначе
// config/app.json). Вторая переменная — историческая: так файл назывался, когда
// в нём жил только LLM.
func AppConfigPath() string {
	for _, env := range []string{"APP_CONFIG_FILE", "LLM_CONFIG_FILE"} {
		if p := os.Getenv(env); p != "" {
			return p
		}
	}
	return filepath.Join("config", "app.json")
}

// ReadAppFile читает файл; нет файла — пустое значение и nil (файла может и не быть).
func ReadAppFile(path string) (AppFile, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return AppFile{}, nil
	}
	if err != nil {
		return AppFile{}, err
	}
	var v AppFile
	if err := json.Unmarshal(raw, &v); err != nil {
		return AppFile{}, err
	}
	return v, nil
}

// WriteAppFile атомично пишет файл (изменения применяются на старте сервиса).
func WriteAppFile(path string, v AppFile) error {
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

// applyAppFile дописывает в Config то, что не задано окружением.
func applyAppFile(c *Config) error {
	f, err := ReadAppFile(AppConfigPath())
	if err != nil {
		return err
	}
	str := func(env string, cur *string, val string) {
		if os.Getenv(env) == "" && val != "" {
			*cur = val
		}
	}
	num := func(env string, cur *int, val *int) {
		if os.Getenv(env) == "" && val != nil {
			*cur = *val
		}
	}
	str("LLM_BASE_URL", &c.LLMBaseURL, f.LLM.BaseURL)
	str("LLM_MODEL", &c.LLMModel, f.LLM.Model)
	if os.Getenv("LLM_ENABLE_THINKING") == "" && f.LLM.EnableThinking != nil {
		c.LLMEnableThinking = *f.LLM.EnableThinking
	}
	str("VOICE_URL", &c.VoiceURL, f.VoiceURL)
	str("SANDBOX_URL", &c.SandboxURL, f.SandboxURL)
	num("SILENCE_NUDGE_S", &c.SilenceNudgeS, f.Timing.SilenceNudgeS)
	num("VAD_END_SILENCE_MS", &c.VADEndSilenceMS, f.Timing.VADEndSilenceMS)
	num("VAD_RMS_THRESHOLD", &c.VADRMSThreshold, f.Timing.VADRMS)
	num("VAD_PRESTT_SILENCE_MS", &c.VADPreSilenceMS, f.Timing.VADPreSilenceMS)
	num("SESSION_PAUSE_TIMEOUT_S", &c.PauseTimeoutS, f.Timing.PauseTimeoutS)
	num("SESSION_LIMIT_S", &c.SessionLimitS, f.Timing.SessionLimitS)
	return nil
}
