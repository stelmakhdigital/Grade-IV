// Package config — настройка сервиса api из переменных окружения (см. infra/.env.example).
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// DevInsecureSecret — значение по умолчанию JWT_SECRET для разработки.
const DevInsecureSecret = "dev-insecure-secret"

// Config — параметры сервиса api.
type Config struct {
	Addr           string // адрес прослушивания (ADDR, ":8000")
	DatabaseURL    string // sqlite:///path | postgres://... (DATABASE_URL)
	JWTSecret      string // секрет подписи JWT (JWT_SECRET)
	JWTExpiryHours int    // время жизни токена, ч (JWT_EXPIRY_HOURS)
	MinutesFreeS   int    // стартовый грант минут, с (MINUTES_FREE_S)
	PauseTimeoutS  int    // пауза дольше порога → aborted, с (SESSION_PAUSE_TIMEOUT_S, SRS §7)
	// SessionLimitS — лимит длительности сессии (SESSION_LIMIT_S):
	// 0 — по грейду (45/50/60/75 мин), >0 — фиксированный, с;
	// переменная "0"/"off" → -1 (без ограничения по времени).
	SessionLimitS   int
	VoiceURL        string // voice-сервис: /api/v1/stt, /api/v1/tts (VOICE_URL)
	LLMBaseURL      string // OpenAI-совместимый LLM (LLM_BASE_URL)
	LLMModel        string // модель LLM (LLM_MODEL)
	LLMAPIKey       string // ключ LLM (LLM_API_KEY)
	SandboxURL      string // sandbox-сервис (SANDBOX_URL)
	LogLevel        string // уровень логов (LOG_LEVEL)
	SilenceNudgeS   int    // тишина > порога → nudge от ИИ, с (SILENCE_NUDGE_S)
	LLMMock         bool   // LLM-мок вместо реального эндпоинта (LLM_MOCK=1; dev/CI, ADR-005)
	VADEndSilenceMS int    // конец реплики по тишине, мс (VAD_END_SILENCE_MS, ADR-002)
	VADRMSThreshold int    // абсолютный мин. порог RMS int16 (VAD_RMS_THRESHOLD); фактический — адаптивный (3×шумовой пол)
	// VADPreSilenceMS — предварительная тишина для pre-STT (VAD_PRESTT_SILENCE_MS,
	// default 400; 0 — отключает pre-STT).
	VADPreSilenceMS int
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// Load читает окружение и валидирует минимум.
func Load() (*Config, error) {
	c := &Config{
		Addr:            getEnv("ADDR", ":8000"),
		DatabaseURL:     getEnv("DATABASE_URL", "sqlite:///./data/grade.db"),
		JWTSecret:       getEnv("JWT_SECRET", DevInsecureSecret),
		JWTExpiryHours:  getEnvInt("JWT_EXPIRY_HOURS", 168),
		MinutesFreeS:    getEnvInt("MINUTES_FREE_S", 3600),
		PauseTimeoutS:   getEnvInt("SESSION_PAUSE_TIMEOUT_S", 1800),
		SessionLimitS:   sessionLimitFromEnv(),
		VoiceURL:        getEnv("VOICE_URL", "http://localhost:8100"),
		LLMBaseURL:      getEnv("LLM_BASE_URL", "http://localhost:8300/v1"),
		LLMModel:        getEnv("LLM_MODEL", "Qwen3-4B"),
		LLMAPIKey:       os.Getenv("LLM_API_KEY"),
		SandboxURL:      getEnv("SANDBOX_URL", "http://localhost:8200"),
		LogLevel:        getEnv("LOG_LEVEL", "info"),
		SilenceNudgeS:   getEnvInt("SILENCE_NUDGE_S", 8),
		LLMMock:         getEnv("LLM_MOCK", "") == "1",
		VADEndSilenceMS: getEnvInt("VAD_END_SILENCE_MS", 900),
		VADRMSThreshold: getEnvInt("VAD_RMS_THRESHOLD", 100),
		VADPreSilenceMS: getEnvInt("VAD_PRESTT_SILENCE_MS", 400),
	}
	if c.JWTExpiryHours <= 0 {
		return nil, fmt.Errorf("JWT_EXPIRY_HOURS должен быть > 0 (получено %d)", c.JWTExpiryHours)
	}
	if c.MinutesFreeS <= 0 {
		return nil, fmt.Errorf("MINUTES_FREE_S должен быть > 0 (получено %d)", c.MinutesFreeS)
	}
	return c, nil
}

// sessionLimitFromEnv — SESSION_LIMIT_S: пусто → 0 (лимит по грейду);
// "0"/"off"/"none" → -1 (без ограничения по времени); число >0 → секунд.
func sessionLimitFromEnv() int {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("SESSION_LIMIT_S")))
	switch raw {
	case "":
		return 0
	case "0", "off", "none":
		return -1
	}
	if v, err := strconv.Atoi(raw); err == nil && v > 0 {
		return v
	}
	return 0
}
