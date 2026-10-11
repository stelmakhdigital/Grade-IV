# Грейд — AI мок-интервью для разработчиков

Онлайн-сервис, где кандидат проводит **мок-интервью с ИИ-интервьюером в реальном времени**:
говорит голосом (ИИ слышит, отвечает голосом, перебивает), решает live-code задачи в
браузерном редакторе с запуском тестов, проектирует системы на whiteboard. По итогам —
детальный отчёт: оценка по критериям с примерами из ответов, разрыв до грейда,
план подготовки на 2 недели, прогресс по прошлым интервью.

**Грейды:** Junior / Middle / Senior / Staff (45/50/60/75 мин) · **Стеки:** Go, Python (MVP) · **Язык:** русский
**Профили интервьюера:** 5 тонов (strict/balanced/supportive/playful/socratic) × 3 сложности — выбирают в кабинете.

## Стек

| Сервис | Язык | Порт | Роль |
|---|---|---|---|
| `services/frontend` | React + TS (Vite) | 5173 | Кабинет, голосовая сессия (Web Audio), Live-Code (Monaco), whiteboard, отчёт |
| `services/api` | Go | 8000 | Авторизация (JWT), сессии, WS-оркестрация голосового диалога, LLM-интервьюер, отчёты, тарификация |
| `services/voice` | Python (FastAPI) | 8100 | STT (**GigaAM v3**, Sber, MIT — по умолчанию; faster-whisper — опция), TTS (Silero v5), Silero VAD, стриминговый STT (WS) |
| `services/sandbox` | Go | 8200 | Песочница кода: запуск решений и тестов (Go/Python) в изолированных subprocess с rlimit |
| LLM | (внешний) | — | Qwen3.8-27B через vLLM (любой OpenAI-совместимый эндпоинт) |

Голосовой контур: микрофон (AudioWorklet → PCM16 16 кГц) → WS → api → `voice:/stt/stream`
(Silero VAD, partial каждые 500 мс, final по тишине 600 мс, лимит 30 с непрерывной речи) →
LLM (интервьюер) → стриминговый TTS с клиуза-диспетчизацией → колонки. Barge-in, пауза/резюме,
режим записи длинных ответов.

## Быстрый запуск

**Требования:** Go ≥ 1.26, Node.js ≥ 20 + pnpm, Python ≥ 3.10, ~6 ГБ диска под модели.
GPU **не обязателен** (STT GigaAM отлично работает на CPU; GPU ускоряет faster-whisper-опцию).

```bash
# 1. Код
git clone https://github.com/stelmakhdigital/Grade-IV && cd Grade-IV

# 2. Зависимости (Go, pnpm, Python venv с torch CPU)
make install

# 3. Модели STT/TTS (~4 ГБ): GigaAM v3 (E2E/CTC/RNNT) + Silero TTS v5 RU
#    (faster-whisper и LLM — опционально, скрипт спросит)
MODELS_DIR=$PWD/services/voice/models bash scripts/download-models.sh

# 4. Запустить весь стек (sandbox + voice + api + frontend)
bash scripts/run-all.sh start        # или: make run-all
bash scripts/run-all.sh status       # состояние; stop / restart — по аналогии
```

Открыть **http://localhost:5173** → зарегистрироваться → провести интервью.

### LLM-узел

Интервьюер — любой **OpenAI-совместимый** эндпоинт (vLLM, llama.cpp, Ollama, облако):

```bash
LLM_BASE_URL=http://YOUR-LLM:8000/v1 LLM_MODEL=qwen3.8-27b-fp8 bash scripts/run-all.sh start
```

Нет LLM-узла? — `LLM_MOCK=1` (детерминированный эхо-интервьюер, удобно для разработки
голосового контура и UI).

### Основные переменные (есть дефолты)

| Переменная | Дефолт | Назначение |
|---|---|---|
| `LLM_BASE_URL` / `LLM_MODEL` | `http://192.168.1.114:8000/v1` / `qwen3.8-27b-fp8` | LLM-эндпоинт |
| `LLM_MOCK` | `0` | `1` — мок-интервьюер без LLM |
| `VOICE_STT_PROVIDER` | `gigaam` | `gigaam` (по умолчанию) / `faster-whisper` / `fake` (CI) |
| `GIGAAM_MODEL` | `v3_e2e_rnnt` | `v3_e2e_rnnt` (пунктуация) / `v3_rnnt` (макс. точность) / `v3_ctc` / `v3_e2e_ctc` / … |
| `STT_DEVICE` | `cuda` | только для faster-whisper; GigaAM: `GIGAAM_DEVICE` (`cpu`/`cuda`/auto) |
| `TTS_SPEAKER` | `aidar` | голос Silero v5: aidar, baya, kseniya, eugene, xenia |
| `DATABASE_URL` | sqlite в `FOR_RUN/` | dev — SQLite; prod — `postgres://…` |

Полный список — в `.env.example`. БД, логи и PID — в `FOR_RUN/` (вне git).

### CPU-only режим (без GPU)

Всё работает на CPU: GigaAM STT ~0.1 с на 3 с аудио. Просто не задавайте `STT_DEVICE=cuda`
(GigaAM сам переключится на CPU с warning; faster-whisper: `STT_DEVICE=cpu STT_COMPUTE_TYPE=int8`).

### Тесты и сборка

```bash
make test     # go vet+test (api, sandbox) + vitest (frontend) + pytest (voice)
make build    # go build + vite build
make smoke    # REST e2e-смоук на живом api
```

## Структура

```
services/
  api/        Go: http API + WS + LLM-интервьюер (internal/{httpapi,interviewer,session,db,…})
  sandbox/    Go: запуск кода/тестов кандидатов (subprocess + rlimit, банк задач 34 шт.)
  voice/      Python: FastAPI — /api/v1/stt, /tts, WS /stt/stream, /vad/stream
  frontend/   React+TS: кабинет, сессия (voice/livecode/design/отчёт)
  models/     (gitignore) веса STT/TTS после download-models.sh
infra/        docker compose, caddy (prod-обёртка), monitoring (prometheus/grafana)
scripts/      run-all.sh (dev-стек), download-models.sh (модели)
FOR_RUN/      (gitignore) dev-БД, логи, PID
```

## Шаблонизация и промпты (A6)

Единый источник правды для текстов программ/промптов интервьюера:

- **Сид (код)** — `services/api/internal/interviewer/prompts.go`: `gradeProgram`
  (программа по грейду), `gradeFocus`, `persona`, `toneBlocks`, `difficultyBlocks`.
  Каждый текст определён **ровно один раз**. `interviewer.DefaultPrograms()` —
  только выводит из них дефолтные шаблоны (не дублирует литералы).
- **База данных** — таблица `interview_templates` (редактируемые через Admin-UI):
  `db.Template.ProgramString()` — форматтер блоков шаблона в текст промпта (изменяемый
  источник, не дублирование сида).

**Runtime-precedence** (закреплён кодом, `session/engine_lifecycle.go`):

1. **Шаблон сессии** — `template_id`, выбранный кандидатом в кабинете (по id)
   или дефолтный для грейда (строка `interview_templates` в БД). Его программа
   фиксируется в сессии при создании.
2. **Строка `interview_templates` (БД)** — та же сущность, выбранная по id/дефолту.
3. **Seed-текст из кода** (`gradeProgram`) — только если строки в БД нет
   (шаблон грейда удалён). При использовании fallback'а пишется **WARN** в лог.

То есть: *выбранный в кабинете шаблон (БД) > дефолтный шаблон грейда (БД) >
seed-программа из кода (fallback + WARN)*.

**Admin-UI** (итерация 11): редактирование ВСЕХ шаблонов проекта (программы,
начала разговора, реестр TTS-голосов) — `GET/PUT /api/v1/admin/*`, доступен
первому зарегистрированному пользователю (флаг `users.is_admin`).

## Лицензия

[Apache 2.0](LICENSE). STT — GigaAM v3 (Sber, MIT) / faster-whisper (MIT); TTS — Silero v5 (MIT).
