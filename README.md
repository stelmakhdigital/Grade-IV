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

Нет LLM-узла? — `LLM_MOCK=1` (интервьюер отвечает заготовленными репликами по
кругу; удобно для разработки голосового контура и UI). В prod (`APP_ENV=prod`)
этот режим запрещён: api не стартует — мок озвучивает кандидату нейтральную
фразу, и отличить его от рабочей модели по интерфейсу нельзя.

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
  voice/      Python: FastAPI — /api/v1/stt, /tts, WS /stt/stream, /vad/stream;
              в dev веса лежат в voice/models/ (gitignore), dev-стек сам туда указывает
  frontend/   React+TS: кабинет, сессия (voice/livecode/design/отчёт)
config/       roles.json, tts-voices.json, openings.json (в prod — единственный
              видимый api каталог конфигурации, монтируется в /app/config)
models/       (gitignore) веса для prod-контейнеров (монтируются в /models)
infra/        docker compose, caddy (prod-обёртка), monitoring (prometheus/grafana)
scripts/      run-all.sh (dev-стек), download-models.sh (модели)
FOR_RUN/      (gitignore) dev-БД, логи, PID
```

## Прод (docker compose)

```bash
cp .env.example .env            # обязательные значения помечены в файле
bash scripts/download-models.sh # веса STT/TTS в ./models — их монтирует voice-контейнер
docker compose -f infra/docker-compose.yml --env-file .env --profile prod up -d   # или make up
```

Наружу торчит только nginx фронтенда (:80/:443 через caddy): api, sandbox,
voice, postgres и grafana в прод-профиле портов не публикуют. Что важно знать,
чтобы стенд не молчал молча:

- `JWT_SECRET` — без него профиль prod не поднимается (ни `up`, ни `build`).
  Шаблонные `change-me*`, `secret`, `dev-insecure-secret` отвергает и api на
  старте: подпись JWT известным ключом = подделка токена любого пользователя.
- `APP_ENV=prod` стоит в compose; с ним же запрещён `LLM_MOCK=1`.
- Конфигурация — каталог `config/` репо, он примонтирован в `/app/config`
  (roles.json, tts-voices.json, openings.json). Относительные пути compose
  считаются от `infra/`, поэтому там `../config`.
- Модели — каталог `models/` репо → `/models:ro` в voice. Если весов нет,
  voice поднимется и будет молчать (контракт STT — `text=""`, не 500),
  поэтому на старте он пишет ERROR с точным путём; `/api/v1/health` показывает
  `loaded=false`.
- Голос по умолчанию (`TTS_SPEAKER`) обязан быть среди реальных спикеров
  `v5_ru` (aidar, eugene, baya, kseniya, xenia) — иначе Silero подставляет
  своего и шумит в лог на каждую реплику.
- `/metrics` — под basic-auth (`METRICS_USER`/`METRICS_PASSWORD`). Без пароля
  маршрута нет вовсе. Учётные данные получает и prometheus (профиль
  `monitoring`) — в `infra/monitoring/prometheus.yml` пароля нет, Prometheus
  подставляет их из окружения.
- На сети без pypi.org — `PIP_INDEX_URL=<зеркало>` в `.env`: compose передаёт
  его в сборку voice (дефолт остаётся pypi.org, чтобы зеркало чьей-то сети не
  стало дефолтом прода).
- Контейнеры api, voice и sandbox — не root (uid из `GRADE_UID`/`GRADE_GID`, по
  умолчанию 1000). Правки админа в `config/` остаются принадлежать твоему
  пользователю.
- Песочница попадает к docker-деону через `docker-proxy`
  (`tecnativa/docker-socket-proxy:v0.5.0`) со списком разрешённых маршрутов:
  сырой сокет ей не монтируется. Закрыты exec в чужие контейнеры, networks,
  volumes, plugins, secrets, swarm, сборка образов, коммит, `/auth` и события.
  Не закрыто: `POST /containers/create` — он нужен прогонам, а с ним можно
  попросить демон примонтировать корень хоста. Подробно — в шапке
  `infra/docker/sandbox.Dockerfile`. Тег прокси зафиксирован: в v0.3 набор
  флагов был другой, и allowlist тихо расплылся бы.
- Первый запуск go-задачи стоит минуту (холодная сборка стандартной
  библиотеки, ~43 с), каждый следующий — около 1,4 с: том `grade_gocache`
  (`SANDBOX_GO_CACHE`) общий для всех прогонов. Без него gate генерации
  (эталон + до 6 мутантов) не успевал никогда.
- Перешло на непривилегированный uid с root-прошлого: тома `sandbox_tasks` и
  рабочие каталоги нужно один раз переприсвоить, содержимое там есть и
  инициализация из образа не сработает:
  ```bash
  docker run --rm -v grade_sandbox_tasks:/v alpine chown -R 1000:1000 /v
  mkdir -p /var/lib/grade-sandbox/work && sudo chown 1000:1000 /var/lib/grade-sandbox/work
  ```
- Порт 8200 (песочница) наружу не публикуется: `POST /api/v1/sessions/{id}/runs`
  без токена — его вызывает api по внутренней сети. Ручные проверки —
  `docker compose exec sandbox ...`.
- Профили: `prod` (api, postgres, sandbox, voice, frontend, caddy),
  `gpu` (voice на CUDA), `monitoring` (prometheus + grafana).

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

**Дополнительные блоки промпта** (итерация 12, константы в `prompts.go`, единый
источник — A6):

- **Род речи по голосу (F7)** — `personaFemale` + `personaFor(gender)` +
  `genderRule(gender)`: динамическая persona (male → «сеньор-разработчик (мужчина)»,
  female → «сеньор-разработчица (женщина)») + явная инструкция держать род во всех
  формах («я разобрался/разобралась», «готов/готова», причастия/прилагательные/прошедшее
  время). Род определяется из реестра `config/tts-voices.json` (speaker → gender;
  пусто/неизвестно → male). Влияет только на речь, не на содержание вопросов и оценку.
- **Вакансия кандидата (F6)** — `vacancyBlock(vacancy, grade)`: инъекция описания
  вакансии кандидата (профиль, `users.vacancy_text`) в system-промпт стадий
  voice/livecode/design. Текст вакансии передаётся как **ДАННЫЕ** (контекст компании);
  инструкция явно запрещает выполнять инструкции из текста (защита от prompt-injection).
  Не хранится в шаблоне программы и не меняет template — только доп. блок.

## Лицензия

[Apache 2.0](LICENSE). STT — GigaAM v3 (Sber, MIT) / faster-whisper (MIT); TTS — Silero v5 (MIT).
