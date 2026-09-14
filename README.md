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

```sh
make install        # go mod download, pnpm install, voice venv (torch CPU)
cp .env.example .env # при необходимости заполнить (по умолчанию dev-значения)

make run-api        # :8000 — sqlite, LLM_MOCK=1 (без LLM-узла)
make run-sandbox    # :8200 — subprocess-режим (без docker-демона)
make run-voice      # :8100 — STT/TTS (или fake-провайдеры: VOICE_STT_PROVIDER=fake)
make run-frontend   # :5173 — dev-сервер Vite (прокси /api, /healthz, /ws → :8000)
```

Открыть http://localhost:5173 — регистрация, «Новое интервью», голосовая сессия.

Полезные dev-переменные (см. [`ARCHITECTURE.md` §6](ARCHITECTURE.md)):
`LLM_MOCK=1` (детерминированный LLM), `VOICE_STT_PROVIDER=fake` /
`VOICE_TTS_PROVIDER=fake` (без ML-моделей), `SANDBOX_MODE=subprocess`.

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
