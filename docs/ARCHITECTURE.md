# Архитектура «Грейд»

Документ о том, как система устроена сейчас. Разделы нумерованы: на `§3`, `§4.1`,
`§4.2`, `§4.3`, `§4.4`, `§6` ссылаются комментарии в коде (около 80 мест), номера
менять нельзя. Решения «почему так» — [DECISIONS.md](DECISIONS.md), эксплуатация —
[OPERATIONS.md](OPERATIONS.md), разработка — [DEV.md](DEV.md).

## 1. Что делает система

Кандидат загружает резюме и нажимает «Новое интервью». ИИ-интервьюер проводит
мок-интервью голосом: 45–75 минут в зависимости от подобранного грейда, три этапа
(голосовой диалог → live-code → system design), в конце детальный отчёт. Один
прогон = одна сессия, все состояния в Postgres.

## 2. Сервисы и потоки

| Сервис | Порт | Назначение |
|---|---|---|
| `services/frontend` | 5173 dev / 80 в контейнере | кабинет, голосовая сессия, Live-Code (Monaco), whiteboard, отчёт |
| `services/api` | 8000 | авторизация, сессии, WS-оркестрация, ИИ-интервьюер, отчёты, учёт времени сессии |
| `services/voice` | 8100 | STT (GigaAM v3 по умолчанию), TTS (Silero v5 RU), Silero VAD, стриминг |
| `services/sandbox` | 8200 | запуск решений и тестов кандидата в изолированных subprocess/docker |
| `infra/caddy` | :80/:443, + :16870 | TLS-терминация и второй незащищённый вход (см. OPERATIONS) |
| LLM (внешний) | — | любой OpenAI-совместимый эндпоинт, по умолчанию Qwen через vLLM |

Наружу из прод-стека публикуются только входы caddy. api, sandbox, voice и postgres
внутренние; мониторинг — профиль `monitoring`, только loopback.

Голосовой тракт одного хода диалога:

```
микрофон (AudioWorklet → PCM16 16 кГц mono)
  → WS /api/v1/ws/sessions/{id}
  → api: буфер речи + voice:/vad/stream (Silero VAD)
  → voice:/stt/stream (partial каждые ~500 мс, final по тишине 600 мс, потолок 30 с речи)
  → final-реплика → LLM (интервьюер) → стрим ответа
  → voice:/tts/stream → чанки аудио в браузер (клик-диспетчер, barge-in)
```

## 3. Доменные модели

`services/api/internal/models` — единственный источник доменных типов: Stage,
Grade, Session, TranscriptLine, Report. REST-DTO и WS-сообщения собираются отсюда,
фронтенд дублирует формы в `src/api.ts`.

- Этапы: `voice` → `livecode` → `design` → `report` (`models.go:67-70`).
- Грейды и длительность: Junior 45, Middle 50, Senior 60, Staff 75 минут
  (`models.go:84-96`). Роль, грейд, стек и набор этапов выбираются по резюме
  кандидата (см. §5.1), а не выбором кандидата в кабинете.
- Профиль интервьюера: тон × сложность, 10 готовых пресетов
  (`internal/db/profiles.go:56-77`), свои профили — через админку.

## 4. Границы

### §4.1 REST api

`internal/httpapi` — все HTTP-хэндлеры, `server.go` — таблица маршрутов и
middleware. Аутентификация JWT (заголовок `Authorization`), первый зарегистрированный
пользователь становится админом (`db/db.go:253`).

Группы маршрутов: `/api/v1/auth/*`, `/api/v1/me/*` (профиль, резюме, вакансия),
`/api/v1/sessions/*` (создание, события, пауза/резюме, whiteboard, runs, отчёт,
публичная ссылка), `/api/v1/tracks/*` (ролевые треки и вопросы),
`/api/v1/admin/*` (треки, шаблоны, открытия, голоса TTS), `/healthz`.

Ошибки домена маппятся в HTTP в одном месте (`error_mapping`), лимиты частоты —
`ratelimit.go` (скользящее окно, 429 + `Retry-After`). Тело запросов ограничено по
размеру (`trackMaxBodyBytes`), резюме читается только как текст.

WS-билет короткоживущий: клиент получает `POST /api/v1/ws-ticket` и поднимает
`GET /api/v1/ws/sessions/{id}?ticket=`, то есть JWT не попадает ни в URL, ни в логи
прокси.

### §4.2 WS-протокол сессии

Один WebSocket на сессию. Исходящие JSON-события: `ai_text`, `stage`, `stage_action`,
`recording`, `error`, а также `run_result` и `whiteboard_saved` для UI Live-Code и
доски. Входящие: `stage_action`, `finish`, `code_run_requested`, `submit_solution`,
`whiteboard_saved`, `recording`, `utterance`. Микрофон идёт бинарными кадрами
PCM16 mono 16 кГц (ADR-001); аудио ответа клиента тоже бинарное.

Реплика, начатая в одном этапе и законченная в другом, относится к этапу на момент
начала (нумерация реплик в отчёте не съезжает).

### §4.3 Клиент voice-сервиса

`internal/voicesvc` — единственный, кто ходит в voice: `stt` (multipart), `vad/stream`
(WS, state-события), `tts` и `tts/stream`. Voice не знает про сессии и пользователей:
провайдеры, модели и стриминг, больше ничего. Дефолты голоса держатся в
`services/voice/app/main.py`, их не дублируют в других сервисах (правило в `AGENTS.md`).

### §4.4 Контракт sandbox

`POST /runs` c телом `{task, stack, solution, tests}` → `Result{passed, tests, output,
timings}`. Задачи встроены в бинарник через `go:embed` (`internal/tasks`), банк —
`tasks.json` (57 задач: go 28, python 27, shell 2), плюс динамические задачи,
сгенерированные под резюме и пропущенные через gate (ADR-010, §5.3).

Изоляция: subprocess с rlimit (CPU, память, размер файла, pid) или docker с
`--network=none --memory=512m --cpus=1 --pids-limit=128` (ADR-003); docker-путь идёт
через socket-proxy с allowlist, сокет докера в песочнице не монтируется. Общий GOCACHE
в named volume, иначе каждая сборка Go стоит 40+ секунд.

## 5. Сессия: как устроен прогон

### §5.1 Подбор по резюме

`POST /api/v1/sessions` принимает профиль интервьюера и голос; роль, грейд, стек,
заголовок и этапы берёт из резюме ИИ (`interviewer`), при пустом или неподходящем
резюме — дефолты трека. Язык(и) из резюме задают стек live-code; остальные языки
уходят в вопросы.

### §5.2 Машина состояний

`internal/session` — движок: состояние сессии, очередь хода, пауза/резюме,
переходы между этапами, терминация. Один гонщик на сессию, всё пишется в
`session_events`, чтобы перезагрузка браузера ничего не теряла. Пауза не увеличивает
учтённое время сессии.

### §5.3 Live-Code

Задача берётся из банка или генерируется под резюме (`internal/taskgen`), проходит
gate: эталонное решение и мутанты прогоняются в песочнице, задача публикуется только
при `ok`. Кандидат пишет в Monaco, запускает тесты, получает `run_result`; финальная
сдача уходит на ИИ-ревью.

### §5.4 System Design

Клиент рисует в Excalidraw, схема хранится как JSON холста
(`PUT/GET /sessions/{id}/whiteboard`, ADR-004). Отсутствие сохранённой схемы —
нормальное состояние (200 с пустым ответом, не 404). На этапе отчёта схема
переводится в структурированное описание и оценивается по рубрике.

### §5.5 Отчёт

`internal/interviewer/report.go`: оценка по критериям с примерами из реплик, разрыв до
грейда, план подготовки. Оценку держит измерение: там, где есть замер (тесты
live-code), ИИ не может поднять балл больше чем на шаг над измеренным
(`capMeasuredScores`). Отчёт отдаётся по `GET /sessions/{id}/report` и по публичной
ссылке `share_token` (F1-export).

## 6. Конфигурация и окружение

Все дефолты живут в коде, `.env` их переопределяет. Единственный шаблон —
корневой `.env.example`; `docker compose` читает `--env-file .env`, относительные
пути внутри compose считаются от `infra/`.

Обязательное для прод-профиля: `JWT_SECRET` (шаблонные значения api отвергает на
старте), `POSTGRES_PASSWORD`, путь к моделям. `LLM_MOCK=1` в прод-профиле запрещён.
Ключевые переменные: `APP_ENV`, `LLM_BASE_URL`, `LLM_MODEL` (пусто → авто-детект по
`/models`), `LLM_MOCK`, `VOICE_STT_PROVIDER`, `GIGAAM_MODEL`, `STT_DEVICE` (cpu),
`TTS_SPEAKER` (xenia), `MODELS_DIR`, `GRADE_UID`/`GRADE_GID`.

| Переменная | Дефолт | Где задан |
|---|---|---|
| `API_PORT` | 8000 | `internal/config/config.go:132` |
| `LLM_BASE_URL` | `http://localhost:8300/v1` | `config.go:140` |
| `LLM_MODEL` | пусто → авто | `config.go:141` |
| `SESSION_LIMIT_S` | потолок сессии | `config.go:187` |
| `VOICE_STT_PROVIDER` | `gigaam` | `services/voice/app/main.py` |
| `STT_DEVICE` | `cpu` | `infra/docker-compose.yml:215` |
| `TTS_SPEAKER` | `xenia` | `infra/docker-compose.yml:222` |
| `APP_CONFIG_FILE` | `config/app.json` | `internal/config/appfile.go` |
| `VOICE_MODELS_FILE` | `config/voice-models.json` | `services/voice/app/modelconfig.py` |
| `VOICE_ADMIN_TOKEN` | пусто запись выключена | voice + api (пробрасывает свой) |

Сверх `.env` — файлы настроек из экрана «Настройки» (ADR-012, ADR-015):
`config/app.json` (LLM, адреса voice/sandbox, пороги тишины/VAD, рамки сессии) и
`config/voice-models.json` (голос; voice принимает его через
`PUT /api/v1/config/models`).
Старшинство в обоих сервисах: **переменная окружения > файл > дефолт кода** —
admin-правка не должна тихо переписывать то, что задано в составе деплоя.
Применение — перезапуском сервиса (модель держится в памяти процесса; voice
целиком влезает в лимит 8 GiB, вторая модель рядом не встанет). Секреты в эти
файлы не пишутся: `LLM_API_KEY` — только окружение.

Контур настроек: `GET /api/v1/settings` — всё, что настраивается, с источником
каждого значения (`env | file | default`) и состоянием после записи
(`applied_now` | `needs_restart` | `shadowed_by_env`); `pending` — список того,
что файл уже просит, а сервис ещё не применил. `PUT /api/v1/settings` — одна
точка записи (свои значения в `config/app.json`, голоса — прокси в voice с
`X-Admin-Token`). Блок `install` — read-only (адрес БД, адрес прослушивания,
уровень логов, факты наличия секретов): секреты не отдаются и не принимаются.
`/admin/runtime` и `/admin/models` — алиасы того же контракта. Недоступный voice
в сводке — строка `{error}` в своём блоке, а не 502 на весь экран.

## 7. Хранилище

Postgres в проде, sqlite в тестах — одна схема на оба (`internal/db`), выбор
диалекта по DSN. Таблицы: `users`, `sessions`, `session_events`, `submissions`,
`reports`, `whiteboards`, `interviewer_profiles`, `interview_templates`;
исторические изменения — `ALTER TABLE` в `db.Migrate` (`db.go:108`), 41 переход,
идемпотентно. Миграции накатываются на старте api, откатов нет — правки схемы
делают вперёд-совместимо.

## 8. Наблюдаемость и границы безопасности

- `GET /healthz` у api, `GET /api/v1/health` у voice (показывает провайдеров и
  `loaded`), Prometheus-метрики с basic-auth, профиль `monitoring` (см. OPERATIONS).
- Лимиты частоты на логин/регистрацию/WS-билеты/запуски кода, лимиты по аккаунту
  независимы от лимитов по IP; `X-Forwarded-For` за nginx best-effort.
- bcrypt cost 12; JWT короткоживущий; WS по билету, не по токену в URL.
- Публичная ссылка на отчёт — по частичному индексу `share_token`, пустой токен не
  индексируется и не ищется.
- Резюме и вакансии пользователей не отдаютcя наружу по REST, кроме `GET /api/v1/me/resume`.
