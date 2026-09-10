// Package config — настройка сервиса api из переменных окружения (см. infra/.env.example).
package config

import (
	"fmt"
	"os"
	"strconv"
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
	VoiceURL       string // voice-сервис: /api/v1/stt, /api/v1/tts (VOICE_URL)
	LLMBaseURL     string // OpenAI-совместимый LLM (LLM_BASE_URL)
	LLMModel       string // модель LLM (LLM_MODEL)
	LLMAPIKey      string // ключ LLM (LLM_API_KEY)
	SandboxURL     string // sandbox-сервис (SANDBOX_URL)
	LogLevel       string // уровень логов (LOG_LEVEL)
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
		Addr:           getEnv("ADDR", ":8000"),
		DatabaseURL:    getEnv("DATABASE_URL", "sqlite:///./data/grade.db"),
		JWTSecret:      getEnv("JWT_SECRET", DevInsecureSecret),
		JWTExpiryHours: getEnvInt("JWT_EXPIRY_HOURS", 168),
		MinutesFreeS:   getEnvInt("MINUTES_FREE_S", 3600),
		VoiceURL:       getEnv("VOICE_URL", "http://localhost:8100"),
		LLMBaseURL:     getEnv("LLM_BASE_URL", "http://localhost:8300/v1"),
		LLMModel:       getEnv("LLM_MODEL", "Qwen3-4B"),
		LLMAPIKey:      os.Getenv("LLM_API_KEY"),
		SandboxURL:     getEnv("SANDBOX_URL", "http://localhost:8200"),
		LogLevel:       getEnv("LOG_LEVEL", "info"),
	}
	if c.JWTExpiryHours <= 0 {
		return nil, fmt.Errorf("JWT_EXPIRY_HOURS должен быть > 0 (получено %d)", c.JWTExpiryHours)
	}
	if c.MinutesFreeS <= 0 {
		return nil, fmt.Errorf("MINUTES_FREE_S должен быть > 0 (получено %d)", c.MinutesFreeS)
	}
	return c, nil
}
