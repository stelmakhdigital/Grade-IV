package httpapi

import "os"

// configCandidates — кандидаты пути к config-файлу относительно CWD: сам путь
// (запуск из services/api — make run-api), services/api/путь (запуск из корня
// репо — scripts/run-all.sh) и ../config (общий каталог конфигурации деплоя:
// его монтирует prod-compose в /app/config, там roles.json, openings.json и
// tts-voices.json). BUG-20261011-012: дефолтные пути config/*.json
// были CWD-зависимы, из-за чего при запуске из корня репо tts-voices/openings
// не находились.
func configCandidates(base string) []string {
	return []string{base, "services/api/" + base, "../" + base}
}

// firstExistingConfig — первый существующий из кандидатов; "" если ни одного.
func firstExistingConfig(candidates []string) string {
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}
