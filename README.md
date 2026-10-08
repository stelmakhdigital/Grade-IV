# Grade-IV — «Грейд»

Онлайн-сервис **мок-интервью для разработчиков**: AI-интервьюер ведёт голосовой
диалог (zoom-call), проводит Live-Code (редактор + запуск тестов в сандбоксе) и
System Design (whiteboard с палитрой архитектурных блоков), по итогам выдаёт
развёрнутый отчёт по критериям грейдов. Язык интерфейса и интервью — русский.

- Грейды: Junior / Middle / Senior / Staff (45 / 50 / 60 / 75 мин), стеки: Go, Python (MVP).
- Тарификация: поминутная, 60 минут бесплатно (SRS §7).
- Методология: SDLC + PMBOK, фазы в [`roadmap.md`](roadmap.md), контекст — [`PROJECT_MEMORY.md`](PROJECT_MEMORY.md).

## Документация

| Файл | Содержимое |
|---|---|
| [`AGENTS.md`](AGENTS.md) | правила работы ИИ-агентов (обязательно читать агенту) |
| [`REQUIREMENTS.md`](REQUIREMENTS.md) | SRS v1.1: FR, NFR, критерии и веса отчёта (§12) |
| [`ARCHITECTURE.md`](ARCHITECTURE.md) | архитектура, диаграммы, контракты REST/WS/voice/sandbox |
| [`docs/adr/`](docs/adr) | ADR-001…007 (транспорт аудио, голосовой конвейер, sandbox, whiteboard, LLM, Go-стек, стриминговый STT) |
| [`roadmap.md`](roadmap.md) | фазы SDLC и work packages со статусами |

## Композиция

```
services/
  api/       Go — REST + WS-оркестратор сессий, VAD-контур голоса,
             интервьюер (LLM), отчёты, тарификация          (:8000)
  sandbox/   Go — выполнение кода/тестов (docker | subprocess),
             банк задач (12)                                 (:8200)
  voice/     Python — STT (faster-whisper) + TTS (Silero v5) (:8100)
  frontend/  React + Vite + TS — SPA: кабинет, голосовая сессия,
             Live-Code (Monaco), System Design (Excalidraw), отчёт
infra/       docker-compose (profiles: prod, gpu), Dockerfile
scripts/     smoke.sh — REST e2e-смоук
```

## Быстрый старт (dev)

Требования: Go ≥ 1.26, Node ≥ 22 + pnpm ≥ 10.1 (в CI — pnpm 10), Python ≥ 3.12.

### Шаг 1. Установить зависимости

```sh
make install        # go mod download, pnpm install, voice venv (torch CPU + ML-стек)
```

### Шаг 2. Скачать модели (STT/TTS)

Модели скачиваются в `services/voice/models/` (в `.gitignore`, не попадает в git) —
именно этот каталог использует `make run-all`:
- **faster-whisper** (STT) → `services/voice/models/stt/Systran/faster-whisper-<размер>/`
  (small ~460 МБ; дефолт run-all — `large-v3`, ~3 ГБ)
- **Silero TTS v5 RU** → `services/voice/models/tts/silero-tts-v5_ru.pt` (~150 МБ)
- **Silero VAD onnx** — в комплекте с faster-whisper (не скачивается)

```sh
MODELS_DIR=$PWD/services/voice/models bash scripts/download-models.sh
# или: MODELS_DIR=$PWD/services/voice/models make models
```

LLM-модель (Qwen3.8-27B, ~60 ГБ) по умолчанию не скачивается — скрипт спросит перед
загрузкой (по умолчанию — «нет»); для dev достаточно `LLM_MOCK=1` (эхо-ответы) или
внешнего LLM-узла (см. Шаг 4).

### Шаг 3. Запустить весь стек (одна команда)

```sh
make run-all        # сборка + sandbox :8200 + voice :8100 + api :8000 + frontend :5173
make status         # состояние (процессы + health)
make stop-all       # остановить всё
```

После перезагрузки ПК достаточно `make run-all` — скрипт сам пересоберёт
бинарники и поднимет сервисы (БД — `FOR_RUN/`, модели — `services/voice/models/`;
обе живут на диске и переживают перезагрузку).

Варианты:
- **без LLM-узла** (детерминированный эхо-интервьюер): `LLM_MOCK=1 make run-all`
- **свой LLM-узел**: `LLM_BASE_URL=http://IP:8000/v1 LLM_MODEL=<имя> make run-all`
- **модели в другом месте**: `MODELS_DIR=<каталог> bash scripts/download-models.sh`
  (каталог по умолчанию скрипта — `/mnt/models`); окружение `STT_MODEL`
  (tiny/small/large-v3) — `STT_MODEL=small make run-all`
- **без GPU**: `STT_DEVICE=cpu STT_COMPUTE_TYPE=int8 make run-all` (дефолт run-all —
  `STT_DEVICE=cuda`, `STT_COMPUTE_TYPE=float16`)
- **время сессии**: по умолчанию в dev-запуске — без ограничения
  (`SESSION_LIMIT_S=off`); вернуть лимит по грейду — `SESSION_LIMIT_S= make run-all`
  (пустое значение) или свой: `SESSION_LIMIT_S=7200 make run-all` (2 часа, в секундах)
- **диагностика микрофона** (когда «ИИ не слышит»): `ENABLE_DEBUG=1` (дефолт run-all)
  включает `POST /debug/mic-report` (приём отчётов из `audio-debug.html` и live-отчётов
  захвата), `VITE_MIC_DEBUG=1 make run-frontend` — live-отчёты каждые 10 с + строка
  `мик[worklet|fallback]: чанки · шаг · max rms · ctx` в UI сессии. В prod оба флага
  держать выключенными (эндпоинт без аутентификации — dev-only).

Логи и PID — в `FOR_RUN/logs/`, `FOR_RUN/pids/`; БД — `FOR_RUN/run.db`.

<details><summary>Вручную, по терминалам (разработка)</summary>

```sh
make run-sandbox    # :8200
STT_MODEL=small STT_DOWNLOAD_ROOT=$PWD/services/voice/models/stt TTS_MODEL_DIR=$PWD/services/voice/models/tts make run-voice
LLM_MOCK=1 make run-api
make run-frontend   # :5173
```

</details>

### Шаг 4. (Опционально) Подключить реальный LLM-узел

Если у вас есть LLM-узел (vLLM + Qwen3.8-27B или любой OpenAI-совместимый сервер),
задайте в api:

```sh
# Вместо LLM_MOCK=1:
LLM_MOCK=0 \
LLM_BASE_URL=http://IP_УЗЛА:8000/v1 \
LLM_MODEL=qwen3.8-27b-fp8 \
make run-api
```

Модель на узле скачается автоматически (HuggingFace) или заранее:
```sh
# На LLM-узле:
huggingface-cli download Qwen/Qwen3.8-27B-Instruct --local-dir /mnt/models/llm
vllm serve Qwen/Qwen3.8-27B-Instruct --port 8000 --max-model-len 32768
```

### Шаг 5. Открыть браузер

Открыть **http://localhost:5173** → регистрация → «Новое интервью» → голосовая сессия.

**Проверка:**
```sh
# REST-смоук (полный контур: регистрация → сессия → report):
make smoke
```

### Полезные dev-переменные (см. [`ARCHITECTURE.md` §6](ARCHITECTURE.md))

| Переменная | Значение | Описание |
|------------|----------|----------|
| `LLM_MOCK` | `1` / `0` | `1` — детерминированный эхо-LLM (без узла), `0` — реальный LLM |
| `LLM_BASE_URL` | `http://IP:8000/v1` | Адрес LLM-узла (OpenAI-совместимый API) |
| `LLM_MODEL` | `qwen3.8-27b-fp8` | Имя модели на узле |
| `STT_MODEL` | `small` / `tiny` / `large-v3` | Модель STT (faster-whisper; дефолт run-all — `large-v3`) |
| `STT_DEVICE` / `STT_COMPUTE_TYPE` | `cuda` / `float16` (run-all) | Без GPU — `cpu` / `int8` |
| `STT_DOWNLOAD_ROOT` | `services/voice/models/stt` | Директория кэша STT-моделей |
| `TTS_MODEL_DIR` | `services/voice/models/tts` | Директория TTS-модели (Silero) |
| `SANDBOX_MODE` | `subprocess` / `docker` | `subprocess` — без docker (dev), `docker` — prod |
| `VOICE_STT_PROVIDER` | `faster-whisper` / `fake` | `fake` — без ML-моделей (детерминизм) |
| `VOICE_TTS_PROVIDER` | `silero` / `fake` | `fake` — без ML-моделей (детерминизм) |

### Остановка сервисов

```sh
make stop-all       # всё, что запущено через make run-all
# вручную (4 терминала): Ctrl+C в каждом
```

## Пауза и возобновление сессии (FR-S7)

Тарификация считает только **активное** время сессии (`active_seconds`); время
паузы не тарифицируется (SRS §7).

**API** (auth: Bearer JWT; маршрут `POST /api/v1/sessions/{id}/{action}` в
`httpapi/server.go`, хендлер `handleSessionAction` в `httpapi/session_handlers.go`):

| Action | Из статуса | Результат |
|---|---|---|
| `pause` | `active` | статус → `paused` (записывается `paused_at`); накопленные до паузы активные секунды сохраняются; клиенту по WS уходит кадр `{"type":"timer","remaining_s":…}`; событие `paused` (`reason: "user"`) |
| `resume` | `paused` | статус → `active`, клиенту по WS — кадр `timer`; интервью продолжается с той же стадии. Если пауза длиннее `SESSION_PAUSE_TIMEOUT_S` (дефолт **1800 с** = 30 мин) — сессия завершается как `aborted` (`reason: "pause_timeout"`), тарифицируется фактическое активное время, клиенту по WS: `{"type":"error","code":"session_aborted"}` |
| `finish` | `active` / `paused` | статус → `finished`, стадия → `report`; финальная тарификация активного времени; старт генерации отчёта (GET report: 202 → 200); клиенту по WS: `timer` (`remaining_s: 0`) и `stage: report` |

**Голос (TTS):** пауза останавливает активный TTS-стрим — клиенту по WS уходит
end-кадр и `tts_stop` (тот же контракт, что barge-in). Завершение (`finish`)
останавливает активный TTS-стрим так же. Стоп-сигнал сессии атомарен относительно
начала хода: ход, начатый до паузы, но ещё не дошедший до начала TTS-стрима, стрим
не стартует; в паузе новые ходы/стримы не стартуют.

**Обрыв связи:** при обрыве WS-соединения движок сам ставит активную сессию в паузу
(`reason: "ws_disconnected"`, `Engine.Detach`); после рестарта api активные сессии без
живого таймера также переводятся в паузу (recovery в `Engine.Attach`).

Коды ошибок: 404 `not_found`, 409 `invalid_state` (запрещённый переход, например
`pause` у завершённой сессии), 500 `internal`.

**UI:** кнопки «Пауза» / «Продолжить» в экране сессии — добавляются в этой итерации
фронтенд-агентом (REST-API уже готов).

Источники: `services/api/internal/session/engine_lifecycle.go` (`Pause/Resume/Detach/abort`),
`engine_timer.go` (тарификация), `internal/config/config.go` (`SESSION_PAUSE_TIMEOUT_S`),
`REQUIREMENTS.md` §7, FR-S7; тесты — `session/engine_test.go`.

## Тесты и сборка

```sh
make test           # go vet + go test (api, sandbox) · vitest (frontend) · pytest (voice)
make build          # go build (api, sandbox) · vite build (frontend)
make smoke          # REST e2e-смоук на живом api (LLM_MOCK=1; адрес — переменная API, дефолт :8877)
```

## Деплой (docker compose)

```sh
make up             # profile prod: api + frontend + voice + sandbox + postgres
make up-gpu         # prod + gpu: voice на GPU-узле (nvidia-container-toolkit)
make down
```

`JWT_SECRET`, `LLM_BASE_URL`, `LLM_MODEL`, `STT_MODEL`, `STT_DEVICE` —
подставляются из окружения/`.env` (шаблон — [`.env.example`](.env.example)).
LLM в проде — vLLM + Qwen3.8-27B (OpenAI-совместимый API, ADR-005).

Prod с TLS (caddy) и мониторингом (Prometheus + Grafana):
`--profile prod --profile monitoring` — гайд по VPS/обновлению/бэкапу: [docs/OPERATIONS.md](docs/OPERATIONS.md).

## Лицензия

MIT — см. [`LICENSE`](LICENSE). Self-hosted стек (faster-whisper, Silero v5,
Qwen — Apache 2.0) — NFR-10.
