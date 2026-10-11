# Grade-IV — План улучшений (итерация 1: анализ)

Дата анализа: 2026-10-11. Анализируемая ветка: `master` @ `1632b23`.
Диапазон: `services/` (api=Go, sandbox=Go, voice=Python, frontend=React/TS), `infra/`, `scripts/`, корневой лендинг, Makefile, CI, README.

## 0. Базовая линия «до» (тесты + сборка)

Полный набор тестов и проверка сборки прогнаны ДО вмешательств. Все проходы зелёные:

| Набор | Команда | Результат |
|---|---|---|
| API (Go) | `go vet ./... && go test -count=1 ./...` | **OK** — все пакеты `ok`; `httpapi` 65.3s (включая load-тест) |
| Sandbox (Go) | `go vet ./... && go test -count=1 ./...` | **OK** — `runner` 52.7s (subprocess/rlimit-пробы) |
| Frontend (TS) | `pnpm tsc --noEmit && vitest run` | **OK** — 113 тестов в 15 файлах; есть предупреждения `act(...)` |
| Frontend сборка | `vite build` | **OK** — 7.6s; ⚠️ чанки >500 kB (376 / 737 kB gzip) |
| Voice (Py) | `VOICE_STT_PROVIDER=fake VOICE_TTS_PROVIDER=fake pytest -q` | **OK** — 38 passed, 10 warnings (Deprecation `asyncio.iscoroutinefunction`) |
| Сборка бинарников | `CGO_ENABLED=0 go build` (api, sandbox) | **OK** |

Заключение: «до»-состояние стабильное и зелёное — хорошую базу для регресс-проверок.
Ниже — приоритизированные улучшения. Приоритеты: **P0** (критично/ближе к пользователю или риск), **P1** (важно), **P2** (хорошо бы).

> Примечание: каталог `bugs/` **пуст** (багов зафиксировано не было). Баги ниже найдены анализом кода, а не из `bugs/`. `data/grade.db` и `FOR_RUN/` — gitignored dev-артефакты, в план не входят.

---

## 1. Баги и надёжность

- **[B1] P1** — `TEMP-DEBUG` код в горячем голосовом контуре: `services/api/internal/httpapi/ws.go:490` (лог RMS PCM16 каждого чанка). Оставляет шум в логах на каждом событии микروفона. Убрать/поставить за флаг debug.
- **[B2] P1** — Frontend build генерирует огромные чанки (`subset-shared` 1.82 MB / 737 kB gzip, `percentages` 1.15 MB). Это Monaco/Excalidraw — грузятся синхронно. Деградация first-load. Решение: lazy `import()` по вкладкам (livecode / design) + `manualChunks`.
- **[B3] P2** — gofmt-несоответствия: `services/api/internal/db/templates.go`, `services/api/internal/httpapi/template_handlers.go` (`gofmt -l` не пустой). CI не ловит (нет `gofmt`/`go vet`-гейта на формат).
- **[B4] P2** — Voice: 10 `DeprecationWarning` (`asyncio.iscoroutinefunction` slated for removal in 3.16). Заменить на `inspect.iscoroutinefunction`. Сейчас Python 3.14 — ещё работает, но ломанёт миграцию.
- **[B5] P1** — `config.go:12` `DevInsecureSecret = "dev-insecure-secret"` — хардкод секрет. Нужна явная проверка, что в prod-режиме dev-секрет не используется (сейчас только по имени).
- **[B6] P2** — `bugs/` пуст, но проект содержит «инциденты» (см. git log: TTS-эхо, stale-бинарник, mic-пауза). Нет процесса фиксации дефектов. Ввести `bugs/BUG-<id>.md` (шаблон: шаги воспроизв., корень, статус) и ссылаться из plan.

## 2. Архитектура

- **[A1] P1** — Гигантский файл голосового пайплайна: `services/api/internal/httpapi/voice_pipeline.go` (1045 строк) + `ws.go` (722). Голосовый контур (VAD→STT→LLM→TTS, barge-in, pause/resume, recording) склеен в httpapi. Выделить в `internal/voiceorchestrator` с явным интерфейсом и DI — улучшает тестируемость (сейчас тесты только integration через server) и читаемость.
- **[A2] P1** — Frontend: `audio/mic.ts` (665) и `hooks/useVoiceSession.ts` (481) — большая императивная логика в React-компонентах/hook. Выделить чистые audio-модули (уже есть `resample.ts`, `player.ts`) от React; добавить контракт-тесты на WS-протокол.
- **[A3] P1** — Тестируемость: нет тестов в `internal/config`, `internal/metrics`, `internal/models` (api) и `sandbox/internal/tasks`. Конфиг и миграции — ключевой код. Добавить unit-тесты (парсинг env, дефолты, валидация; idempotency миграций).
- **[A4] P2** — Наблюдаемость: есть `internal/metrics` (без тестов) и `infra/monitoring` (prometheus/grafana), но связь сервисов с метриками разрозненна. Определить единый набор business-метрик (latency STT/TTS, WER proxy, длительность сессии, fallback) и healthchecks.
- **[A5] P2** — Корневой статический лендинг (`index.html`/`script.js`/`styles.css`) дублирует бренд, но расходится с продуктом: лендинг рекламирует стеки «Go, Python, JS/TS, Java, Rust и др.», а продукт MVP — **Go, Python** (README). Расхождение вводит в заблуждение. Решение: синхронизировать лендинг с реальностью (или явно пометить «скоро») и/или перенести в `services/frontend` как landing-роут.
- **[A6] P2** — Дублирование/двойная «правда»: промпты интервьюера и шаблоны дублируются между `internal/interviewer/prompts.go`, `templates.go` и БД-шаблонами (`db/templates.go`). Определить единый источник (БД как источник, код — дефолты/seed) и описать precedence.

## 3. Тесты

- **[T1] P1** — Закрыть пробелы покрытия из [A3]: `config`, `metrics`, `models`, `sandbox/tasks` (банк задач 34 шт. — проверить целостность: наличие тестов на каждую задачу/стек).
- **[T2] P1** — Frontend: убрать `act(...)`-предупреждения в `SessionView.test.tsx` (несколько state-обновлений вне `act`). Добавление контракт-тестов WS-клиента (`ws.ts`) на реальные event-наборы (сейчас `unknown_ui_event` покрыт точечно).
- **[T3] P2** — E2E: `make smoke` (REST) есть, но голосовой контур e2e покрывается только пробами (`FOR_RUN/probe-*.py`, вне CI). Вынести 1 детерминированный голосовой e2e (fake-провайдеры) в CI.
- **[T4] P2** — Go-гейт в CI: добавить `gofmt -l` и `go vet` как отдельные быстрые шаги (ловить [B3]); сейчас `go vet` только как часть.
- **[T5] P2** — Load/нагрузочный тест в `httpapi` (65s) — зафиксировать целевые метрики (p95 latency, RPS) и алерт-порог, чтобы регрессия производительности была видна.

## 4. Новые фичи (приоритетные)

- **[F1] P1** — Экспорт/поделиться отчётом кандидата (PDF/публичная ссылка) — высокий продукт-вклад, отчёт уже детальный (`report.go`, `ReportView.tsx`).
- **[F2] P1** — Прогресс по прошлым интервью (упомянут в README как цель, проверить реализацию в `runs_handlers.go`/`ReportView` и достроить тренды по грейд-критериям во времени).
- **[F3] P2** — Больше стеков live-code (сейчас Go, Python) — расширить `sandbox/internal/tasks` + шаблоны.
- **[F4] P2** — Onboarding/демо-интервью (автотур без регистрации) для конверсии с лендинга.
- **[F5] P2** — Настройка TTS-голоса/скорости кандидатом (voice `TTS_SPEAKER` уже параметр — поднять в UI кабинета).

## 5. Операции / CI / Docs

- **[O1] P2** — Docker-пробы сандбокса (`ci-docker-test.sh`) выключены из CI (см. git log `d7ca71d`). Вернуть в CI как отдельный быстрый джоб (network=none, rlimit) или явно задокументировать почему выключено.
- **[O2] P2** — Version pinning: Go (go.mod есть), Node (`.nvmrc`/engines — добавить), Python (venv, но не pinned в `requires-python`/`pyproject`). Добавить `.nvmrc`, `pyproject requires-python`, lockfile-проверку.
- **[O3] P2** — Docs: README актуален и хорош. Недостающие: API reference, troubleshooting (голосовой контур), ADR (архитектурные решения: GigaAM выбор, 30c-лимит, barge-in). Ввести `docs/` (tutorials/how-to/reference/explanation по worker_roles.md).
- **[O4] P2** — Graceful shutdown + healthchecks для всех сервисов проверить в `infra/docker-compose.yml` (healthcheck на api/sandbox/voice) и в `run-all.sh`.

## 6. Предлагаемый порядок исполнения (следующие итерации)

1. **P0/P1 быстрая гигиена** (1 итерация): B1 (убрать TEMP-DEBUG), B3 (gofmt), B4 (deprecations), B5 (prod-секрет guard), T4 (CI gofmt/vet gate).
2. **P1 производительность/UX** (1 итерация): B2 (code-splitting frontend).
3. **P1 архитектура голосового контура** (1–2 итерации): A1 (вынести voiceorchestrator) + A2 (frontend audio-модули) + T2 (WS контракт-тесты).
4. **P1 тесты** (1 итерация): A3/T1 (config/metrics/models/tasks), T3 (голос e2e в CI).
5. **P1 фичи**: F1 (экспорт отчёта), F2 (прогресс по времени).
6. **P2**: A5 (лендинг), A6 (промпты), O1–O4, F3–F5.

Каждая итерация: отдельные коммиты, тесты до/после, скриншоты при UI-изменениях, merge в master только после зелёных интеграционных тестов (по `task.md`).
