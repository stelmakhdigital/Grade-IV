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
| [`docs/adr/`](docs/adr) | ADR-001…006 (голосовой контур, VAD, sandbox, whiteboard, LLM, Go-стек) |
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

Требования: Go ≥ 1.26, Node ≥ 22 + pnpm ≥ 12, Python ≥ 3.12.

### Шаг 1. Установить зависимости

```sh
make install        # go mod download, pnpm install, voice venv (torch CPU + ML-стек)
```

### Шаг 2. Скачать модели (STT/TTS)

Модели скачиваются в `FOR_RUN/` (в `.gitignore`, не попадает в git):
- **faster-whisper small** (STT) → `FOR_RUN/stt/Systran/faster-whisper-small/` (~460 МБ)
- **Silero TTS v5 RU** → `FOR_RUN/tts/silero-tts-v5_ru.pt` (~150 МБ)
- **Silero VAD onnx** — в комплекте с faster-whisper (не скачивается)

```sh
make models         # или: MODELS_DIR=$PWD/FOR_RUN bash scripts/download-models.sh
```

LLM-модель (Qwen3.8-27B, ~60 ГБ) не скачивается (опционально в скрипте) — для dev
достаточно `LLM_MOCK=1` (эхо-ответы) или внешний LLM-узел (см. Шаг 4).

### Шаг 3. Запустить весь стек (одна команда)

```sh
make run-all        # сборка + sandbox :8200 + voice :8100 + api :8000 + frontend :5173
make status         # состояние (процессы + health)
make stop-all       # остановить всё
```

После перезагрузки ПК достаточно `make run-all` — скрипт сам пересоберёт
бинарники и поднимет сервисы (модели и БД живут в `FOR_RUN/` и переживают
перезагрузку).

Варианты:
- **без LLM-узла** (детерминированный эхо-интервьюер): `LLM_MOCK=1 make run-all`
- **свой LLM-узел**: `LLM_BASE_URL=http://IP:8000/v1 LLM_MODEL=<имя> make run-all`
- **модели в другом месте**: `bash scripts/download-models.sh` скачивает в
  `FOR_RUN/`; окружение `STT_MODEL` (tiny/small/large-v3) — `STT_MODEL=small make run-all`

Логи и PID — в `FOR_RUN/logs/`, `FOR_RUN/pids/`; БД — `FOR_RUN/run.db`.

<details><summary>Вручную, по терминалам (разработка)</summary>

```sh
make run-sandbox    # :8200
STT_MODEL=small STT_DOWNLOAD_ROOT=$PWD/FOR_RUN/stt TTS_MODEL_DIR=$PWD/FOR_RUN/tts make run-voice
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
LLM_MODEL=qwen3.8-27b-dflash2 \
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
| `LLM_MODEL` | `qwen3.8-27b-dflash2` | Имя модели на узле |
| `STT_MODEL` | `small` / `tiny` / `large-v3` | Модель STT (faster-whisper) |
| `STT_DOWNLOAD_ROOT` | `FOR_RUN/stt` | Директория кэша STT-моделей |
| `TTS_MODEL_DIR` | `FOR_RUN/tts` | Директория TTS-модели (Silero) |
| `SANDBOX_MODE` | `subprocess` / `docker` | `subprocess` — без docker (dev), `docker` — prod |
| `VOICE_STT_PROVIDER` | `faster-whisper` / `fake` | `fake` — без ML-моделей (детерминизм) |
| `VOICE_TTS_PROVIDER` | `silero` / `fake` | `fake` — без ML-моделей (детерминизм) |

### Остановка сервисов

```sh
make stop-all       # всё, что запущено через make run-all
# вручную (4 терминала): Ctrl+C в каждом
```

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

## Лицензия

MIT — см. [`LICENSE`](LICENSE). Self-hosted стек (faster-whisper, Silero v5,
Qwen — Apache 2.0) — NFR-10.
