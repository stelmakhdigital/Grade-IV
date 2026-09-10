# ADR-006. Язык бекэнда: Go (api, sandbox), voice — Python

**Статус:** принято (2026-09-09)

## Контекст
Ядро сервиса (сессии, WS-протокол, тарификация, отчёты, банк задач, сандбокс) ведётся на Go
(решение пользователя, 2026-09-09). Голосовой сервис использует faster-whisper и Silero v5 —
библиотеки только для Python (экосистема torch/CTranslate2).

## Решение
- **api и sandbox — Go** (отдельный go.mod на сервис). Стек: stdlib `net/http` + ServeMux
  (паттерны Go 1.22+), `log/slog` (JSON-логи), `golang-jwt/v5`, `golang.org/x/crypto` (bcrypt),
  `database/sql`: `modernc.org/sqlite` (dev, чистый Go, без cgo) / `pgx` (prod, PostgreSQL).
  WebSocket — `nhooyr.io/websocket` (WP-3).
- **voice — Python (FastAPI)**: единственное ML-исключение; изолирован за `/api/v1/stt|tts`
  и конфигом `VOICE_URL` (ARCHITECTURE §4.3). Go-бекэнд зависит от него только по HTTP —
  подмена провайдеров STT/TTS (решение #16) сохраняется.
- **sandbox (Go)**: управление контейнерами через docker CLI/API, лимиты по ADR-003;
  dev-fallback — exec с ограничениями (WP-6).

## Альтернативы
- **Python-бекэнд (FastAPI)**: проще для ML-склейки, но отклонено решением пользователя
  (2026-09-09): «сам бекэнд — на Go».
- **Go + cgo/whisper.cpp (STT в-процессе)**: отказ от отдельного voice-сервиса — отклонено:
  сложность cgo-сборок, torch для Silero в Go несовместим, теряется чистая точка подмены
  STT-провайдера (`/api/v1/stt`).
- **go-torch для Silero в Go**: тяжёлая экзотичная зависимость, выигрыша для MVP нет.

## Последствия
- + Go-бекэнд: статические бинарники, низкий RAM, высокая конкурентность WS-оркестратора,
  деплой = бинарник + БД.
- + Voice-сервис сохраняет полный ML-экосистемный стек (faster-whisper, Silero, в будущем
  streaming ASR) без компромиссов.
- − Два языка в репозитории: разные тулчейны тестов (go test, pytest, vitest) — единая
  точка запуска через Makefile (WP-1).
- − Без ORM: DDL/миграции — в `internal/db` api (схемы 7 таблиц из ARCHITECTURE §3).
