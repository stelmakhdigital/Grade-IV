# Грейд — AI мок-интервью для разработчиков

Кандидат проводит **мок-интервью с ИИ-интервьюером в реальном времени**: говорит
голосом (ИИ слышит, отвечает голосом, перебивает), решает live-code задачи в
браузерном редакторе с запуском тестов, проектирует систему на whiteboard. Итог —
отчёт: оценка по критериям с примерами из ответов, разрыв до грейда, план подготовки.

Роль, грейд, стек и этапы интервью **подбираются по резюме** кандидата — выбирать в
кабинете нечего. Грейды Junior/Middle/Senior/Staff (45/50/60/75 мин), языки Go,
Python плюс shell-практика, язык интерфейса и диалога — русский. Профилей
интервьюера 10 готовых (тон × сложность), свои — в админке.

| Сервис | Язык | Порт | Роль |
|---|---|---|---|
| `services/frontend` | React + TS (Vite) | 5173 | кабинет, голосовая сессия, Live-Code (Monaco), whiteboard, отчёт |
| `services/api` | Go | 8000 | JWT, сессии, WS-оркестрация, ИИ-интервьюер, отчёты, учёт времени сессии |
| `services/voice` | Python (FastAPI) | 8100 | STT (GigaAM v3 по умолчанию), TTS (Silero v5), VAD, стриминг по WS |
| `services/sandbox` | Go | 8200 | запуск решений и тестов в изолированных subprocess/docker |
| LLM | внешний | — | любой OpenAI-совместимый эндпоинт (по умолчанию Qwen через vLLM) |

## Быстрый запуск (dev)

Нужны Go 1.26, Node 22 + pnpm 10, Python 3.12, ~600 МБ под модели. GPU не обязателен:
STT GigaAM на CPU узнаёт 3 с речи за ~0,1 с.

```bash
git clone https://github.com/stelmakhdigital/Grade-IV && cd Grade-IV
make install                                  # Go-модули, pnpm, venv voice (torch CPU)

# веса STT/TTS; в dev стек ищет их в services/voice/models
MODELS_DIR=$PWD/services/voice/models bash scripts/download-models.sh

make run-all                                  # sandbox + voice + api + frontend
make status                                   # состояние; make stop-all — остановить
```

Открыть http://localhost:5173, зарегистрироваться (первый пользователь — админ),
нажать «Новое интервью».

Интервьюер — любой OpenAI-совместимый эндпоинт:

```bash
LLM_BASE_URL=http://YOUR-LLM:8000/v1 LLM_MODEL=qa-chat make run-all
```

Нет эндпоинта — `LLM_MOCK=1`: интервьюер отвечает заготовленными репликами,
этого хватает для разработки голоса и UI. В проде режим запрещён (api не стартует):
отличить мок от рабочей модели по интерфейсу нельзя.

| Переменная | Дефолт | Назначение |
|---|---|---|
| `LLM_BASE_URL` / `LLM_MODEL` | `http://localhost:8300/v1` / пусто → авто по `/models` | LLM-эндпоинт |
| `LLM_MOCK` | `0` | `1` — мок-интервьюер |
| `VOICE_STT_PROVIDER` | `gigaam` | `gigaam` / `faster-whisper` / `fake` (CI) |
| `GIGAAM_MODEL` | `v3_e2e_rnnt` | вариант GigaAM v3 |
| `STT_DEVICE` | `cpu` | только faster-whisper; для GigaAM — `GIGAAM_DEVICE` |
| `TTS_SPEAKER` | `xenia` | спикер Silero v5 RU: aidar, eugene, baya, kseniya, xenia |
| `DATABASE_URL` | sqlite в `FOR_RUN/` | dev — SQLite, prod — Postgres |

Полный список — `.env.example`, справочник — `docs/DEV.md`. БД, логи и PID dev-стека
— в `FOR_RUN/` (вне git).

```bash
make test     # go vet+test (api, sandbox), токены, vitest, pytest
make build    # бинарники + dist фронтенда
make smoke    # REST e2e на поднятом стенде
```

## Прод

```bash
cp .env.example .env                        # обязательное помечено в файле
bash scripts/download-models.sh             # веса в ./models — их монтирует voice
docker compose -f infra/docker-compose.yml --env-file .env --profile prod up -d --build
```

`make up` то же на переднем плане, `make down` — остановить. Наружу публикуются
только входы caddy: `:80`/`:443` и второй вход `:16870` без TLS (доступ по адресу
машины в локальной сети; там не работает микрофон и трафик открытый — как убрать,
описано в `docs/OPERATIONS.md`). Коротко о том, без чего стенд молчит: `JWT_SECRET`
обязателен и шаблонные значения отвергаются, `LLM_MOCK` в проде запрещён, без
весов STT интервьюер молчит (`/api/v1/health` покажет `loaded=false`), песочница
ходит к докер-демону только через socket-proxy с allowlist.

## Где что лежит

```
services/
  api/        Go: http + WS + ИИ-интервьюер (internal/{httpapi,interviewer,session,db,…})
  sandbox/    Go: запуск кода и тестов, банк задач (internal/tasks) + генератор (taskgen)
  voice/      Python: STT/TTS/VAD, WS /stt/stream и /vad/stream (веса в dev — voice/models/)
  frontend/   React+TS: кабинет, сессия (voice/livecode/design), отчёт, админка
config/       roles.json, tts-voices.json, openings.json (в прод — /app/config)
infra/        compose, caddy, docker-образы, мониторинг
scripts/      run-all.sh, download-models.sh, smoke.sh, check-tokens.mjs
docs/         устройство, решения, эксплуатация, разработка, история
index.html    лендинг (отдельно от приложения)
FOR_RUN/      (gitignore) dev-БД, логи, PID
```

## Документация

- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — сервисы, WS-протокол, границы, модель
  данных, конфигурация. На разделы `§3`, `§4.x`, `§6` ссылаются комментарии в коде.
- [docs/DECISIONS.md](docs/DECISIONS.md) — что стоит за номерами `ADR-00N`.
- [docs/OPERATIONS.md](docs/OPERATIONS.md) — развертывание, входы, мониторинг,
  миграции, частые симптомы.
- [docs/DEV.md](docs/DEV.md) — тулчейн, команды, тесты, конвенции.
- [docs/history/ITERATIONS.md](docs/history/ITERATIONS.md) — что и в каком порядке
  здесь делали (история, не план); разборы инцидентов — `docs/history/bugs/`.
- `AGENTS.md` — правило про STT-провайдер (GigaAM по умолчанию), обязательно к чтению
  правкам голоса.

## Лицензия

[Apache 2.0](LICENSE). STT — GigaAM v3 (Sber, MIT) / faster-whisper (MIT); TTS — Silero v5 (MIT).
