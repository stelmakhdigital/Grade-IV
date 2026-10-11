package config

import (
	"strings"
	"testing"
)

// setEnv — устанавливает APP_ENV и JWT_SECRET на время подтеста (t.Setenv чистит после).
// Пустая строка в JWT_SECRET имитирует «не задан» → getEnv вернёт дефолт (DevInsecureSecret).
func setEnv(t *testing.T, appEnv, jwtSecret string) {
	t.Helper()
	if appEnv == "" {
		t.Setenv("APP_ENV", "")
	} else {
		t.Setenv("APP_ENV", appEnv)
	}
	t.Setenv("JWT_SECRET", jwtSecret)
}

// TestLoadProdRejectsDevSecret (B5): в prod dev-секрет JWT запрещён — Load возвращает ошибку.
func TestLoadProdRejectsDevSecret(t *testing.T) {
	cases := []struct {
		name      string
		appEnv    string
		jwtSecret string
	}{
		{"prod: JWT_SECRET не задан (дефолт = dev-секрет)", "prod", ""},
		{"prod: JWT_SECRET явный dev-секрет", "prod", DevInsecureSecret},
		{"production: dev-секрет", "production", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setEnv(t, tc.appEnv, tc.jwtSecret)
			c, err := Load()
			if err == nil {
				t.Fatalf("ожидалась ошибка при prod + dev-секрет, получено nil (cfg.Env=%q, JWTSecret=%q)",
					c.Env, c.JWTSecret)
			}
			if !strings.Contains(err.Error(), "JWT_SECRET") {
				t.Fatalf("ошибка не описывает причину (JWT_SECRET): %v", err)
			}
		})
	}
}

// TestLoadProdAllowsRealSecret (B5): в prod реальный (не dev) секрет допускается.
func TestLoadProdAllowsRealSecret(t *testing.T) {
	setEnv(t, "prod", "a-real-secret-that-is-not-dev-insecure")
	c, err := Load()
	if err != nil {
		t.Fatalf("prod + реальный секрет: не ожидалась ошибка: %v", err)
	}
	if c.Env != "prod" {
		t.Fatalf("cfg.Env = %q, ожидаю %q", c.Env, "prod")
	}
	if c.JWTSecret != "a-real-secret-that-is-not-dev-insecure" {
		t.Fatalf("cfg.JWTSecret = %q", c.JWTSecret)
	}
}

// TestLoadDevAllowsDevSecret (B5): в dev (default) dev-секрет по-прежнему разрешён (безопасный дефолт).
func TestLoadDevAllowsDevSecret(t *testing.T) {
	cases := []struct {
		name      string
		appEnv    string
		jwtSecret string
	}{
		{"dev (не задан)", "", ""},
		{"dev явный", "dev", ""},
		{"dev явный dev-секрет", "dev", DevInsecureSecret},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setEnv(t, tc.appEnv, tc.jwtSecret)
			c, err := Load()
			if err != nil {
				t.Fatalf("dev + dev-секрет: не ожидалась ошибка: %v", err)
			}
			if c.JWTSecret != DevInsecureSecret {
				t.Fatalf("ожидался dev-секрет по умолчанию, получено %q", c.JWTSecret)
			}
		})
	}
}

// TestIsProdEnv — прямые проверки распознавания prod-среды.
func TestIsProdEnv(t *testing.T) {
	yes := []string{"prod", "production", "live", "PROD"}
	no := []string{"dev", "", "test", "staging", "Prod-x"}
	for _, e := range yes {
		if !isProdEnv(strings.ToLower(e)) {
			t.Errorf("isProdEnv(%q) = false, ожидаю true", e)
		}
	}
	for _, e := range no {
		if isProdEnv(strings.ToLower(e)) {
			t.Errorf("isProdEnv(%q) = true, ожидаю false", e)
		}
	}
}
