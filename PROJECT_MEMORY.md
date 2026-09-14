# PROJECT_MEMORY.md — контекст проекта для ИИ-агента

> Файл для восстановления контекста в новом треде. Обновляется в конце каждого шага.
> Формат записи: дата → что произошло → что решено/что открыто.

## Проект
**«Грейд»** — онлайн-сервис мок-интервью для разработчиков (директория `/Users/budaev/Code/INTERVIEW`).
Ядро — AI-сервис проведения мок-интервью:
1. **Голосовое интервью с ИИ** (TTS — ИИ говорит, STT — слушает кандидата; формат «zoom-call»).
2. **Live-Code** (живое кодинг-интервью): онлайн-редактор кода, задача, сандбокс (код + тесты), ИИ-ревьюер с комментариями и follow-up вопросами.
3. **System Design** (Middle/Senior): whiteboard — готовые архитектурные блоки + свободное рисование; ИИ оценивает схему и устный ответ.

Роли: один ИИ-помощник играет проектного менеджера, продуктового менеджера, аналитика,
архитектора, программиста и тестировщика.

## Существующий артефакт (критично помнить)
- В корне лежит **лендинг «Грейд»** (`index.html`, `styles.css`, `script.js`): vanilla JS, без зависимостей,
  тёмная developer-тема. На лендинге заявлено: «честный разбор от действующего инженера через 24 часа»
  (т.е. на момент прототипа — живые интервью с человеком).
- Продуктовые данные из прототипа (источник правды по ассортименту):
  - Грейды: Junior (45 мин), Middle (50 мин), Senior (60 мин), Staff/Lead (75 мин) — с блоками программы.
  - Стеки: Go, Python, JS/TS, Java, C++, Rust, PHP, Kotlin (у каждого — детали темы).
  - System Design в программе уже заложен: у Middle — «основы», у Senior — «highload-сервис с нуля»,
    у Staff — «кросс-системный design: масштаб ×10».
- Отсюда ключевой продуктовый вопрос: AI-интервью **заменяет** человеческое (с лендинга) или
  **дополняет** его (AI-сессия + опциональный разбор человеком)? → в опросе Discovery.

## Локальное окружение (dev-машина — только для разработки, 2026-09-09)
- Mac14,2 (M2, arm64), macOS 26, **16 ГБ RAM, ~11 ГБ свободно на диске**, 8 ядер.
- Python 3.12, Node 22, pnpm 11, Docker Desktop (демон запускается по требованию), ffmpeg,
  git (SSH-ключ к GitHub работает).
- Сеть: PyPI / HuggingFace / npm / GitHub — доступны.
- Следствие (только для dev): Qwen3.8-27B на эту машину не помещается — dev-LLM: llama.cpp +
  Qwen3-4B, dev-STT: faster-whisper `small`. **Рантайм-цель prod — отдельные серверы:
  LLM и голосовые модели (STT/TTS) — на отдельном сервере в ЛВС (AI-узел), приложение —
  на app-узле (решение #20, ADR-005, ARCHITECTURE §2.1).**

## Ключевые решения
| # | Дата | Решение |
|---|------|---------|
| 1 | 2026-09-09 | Вести проект по фазам SDLC, начиная с Discovery; учитывать PMBOK («РМВОК» — интерпретация агента, подтверждено в опросе) |
| 2 | 2026-09-09 | Контекст — в PROJECT_MEMORY.md, задачи — в roadmap.md, правила — в AGENTS.md |
| 3 | 2026-09-09 | Коммиты — только по явной команде пользователя |
| 4 | 2026-09-09 | «RTS» в ТЗ = STT/ASR: сервис — полноценный голосовой диалог (TTS — ИИ говорит, STT — слушает кандидата) |
| 5 | 2026-09-09 | Лендинг «Грейд» — только пример; продукт — чистое ИИ мок-интервью (без человека в контуре) |
| 6 | 2026-09-09 | Язык интервью и голос ИИ: только русский |
| 7 | 2026-09-09 | Голосовой режим: естественный живой диалог — интонации, паузы, эмоциональность; при долгом молчании кандидата ИИ сам заполняет паузу («всё понятно?», «расскажите ещё») |
| 8 | 2026-09-09 | Ассортимент: 8 стеков × 4 грейда (как на лендинге); MVP — Go и Python |
| 9 | 2026-09-09 | Вторая стадия — **Live-Code** (живой кодинг в редакторе + ИИ-ревьюер), а не «Code Review» |
| 10 | 2026-09-09 | System Design: гибрид — палитра готовых архитектурных блоков + свободное рисование на холсте; ИИ оценивает схему + устный ответ |
| 11 | 2026-09-09 | MVP: все стадии (голос, Live-Code, System Design, отчёт) — для Go и Python |
| 12 | 2026-09-09 | LLM-интервьюер: Qwen3.8-27B (self-hosted; лицензия Apache 2.0 — коммерческое использование OK, проверено 2026-09-09; модель с vision — может оценивать схемы whiteboard по картинке) |
| 13 | 2026-09-09 | TTS/STT: продакшн-подход (не «коробочный» MVP), только лицензионно-чистые для коммерции компоненты; провайдеры — через слой абстракции (рекомендация: streaming STT — Deepgram / Yandex SpeechKit; TTS — gpt-4o-mini-tts / ElevenLabs; на горизонте self-hosted CosyVoice 2, Apache-2.0) |
| 14 | 2026-09-09 | Бизнес-модель: поминутная тарификация, 60 минут бесплатно на аккаунт; платёжный шлюз в MVP не нужен — только учёт времени и лимит |
| 15 | 2026-09-09 | Платформа: Web (браузер) — подтверждено пользователем |
| 16 | 2026-09-09 | Голосовой стек — только self-hosted (без облаков): STT — faster-whisper large-v3-russian на GPU (альтернатива whisper.cpp); TTS — Silero v5 (MIT); архитектура: эндпоинты /api/v1/stt и /api/v1/tts + абстракция провайдера для подмены в будущем. gpt-4o-mini-tts отклонён (только облачный API) |
| 17 | 2026-09-09 | SRS v1.0 (REQUIREMENTS.md) одобрена пользователем (phase gate Requirements); «минута интервью» = активное время сессии (REQUIREMENTS.md §7) |
| 18 | 2026-09-09 | Фаза Design: ADR-001…005 — транспорт WS + PCM16 16 кГц; STT по репликам + VAD (Silero) в оркестраторе api; Docker-контейнер на сессию (лимиты, без сети); Excalidraw + палитра 12 блоков; LLM — OpenAI-совместимый (vLLM prod / llama.cpp dev / Mock CI) — docs/adr/ |
| 19 | 2026-09-09 | Dev-машина (16 ГБ RAM / 11 ГБ диск): dev-LLM — llama.cpp + Qwen3-4B Q4, dev-STT — `small`; prod — vLLM + Qwen3.8-27B (GPU) + large-v3-russian (ADR-005) |
| 20 | 2026-09-09 | Деплой: рантайм-цель — отдельные серверы (dev-машина не используется); LLM (Qwen3.8-27B) и голосовые модели STT/TTS — на отдельном сервере в локальной сети (AI-узел); приложение (api/sandbox/frontend/БД) — на app-узле; связь по ЛВС через конфиг (`LLM_BASE_URL`, `VOICE_URL`) |
| 21 | 2026-09-09 | Язык бекэнда: **Go** (api, sandbox — ADR-006); Python — только voice-сервис (faster-whisper/Silero — ML, torch), изолирован за /api/v1/stt|tts + VOICE_URL |
| 22 | 2026-09-13 | WP-3: WS-auth — JWT в `?token=` (браузеры) или `Authorization: Bearer`; 401 до апгрейда. Обрыв WS → сессия `paused` (FR-S7); пауза > SESSION_PAUSE_TIMEOUT_S (по умолч. 30 мин) → `aborted` + списание факт. времени (SRS §7) |
| 23 | 2026-09-13 | WP-3: тарификация — начисляется только активное время; pause/обрыв/пауза не тарифицируются; списание в minutes_ledger при финализации (finished/aborted); 402 при 0 минут на создание |
| 24 | 2026-09-13 | WP-3: переход на стадию `report` финализирует сессию (finished) — совпадает с моментом «отчёт сгенерирован» из SRS §7 (генерация отчёта — WP-11) |
| 25 | 2026-09-14 | WP-6: MVP-отклонение от ADR-003 — контейнер на каждый run (упрощение; long-lived контейнер на сессию — бэклог Operations). Docker недоступен в dev-среде → дефолт `SANDBOX_MODE=subprocess` (dev), prod — docker (fail-closed 503 без демона) |
| 26 | 2026-09-14 | WP-6: рабочие каталоги sandbox **не в /tmp** (Go игнорирует go.mod в системном temp-root, golang.org/issue/26708) — базовый каталог `~/.local/share/grade-sandbox` (override: `SANDBOX_WORKDIR_BASE`); TMPDIR в env запуска не ставится |
| 27 | 2026-09-14 | WP-6: банк задач — 12 задач (6 Go + 6 Python), go:embed, только stdlib (GOPROXY=off, --network=none); тесты задач пишутся на `pytest` (Python) и `go test -json` (Go); в этой среде pytest стоит колёсами в `~/.local/pydeps` (нет pip), subprocess-раннер прокидывает PYTHONPATH |
| 28 | 2026-09-14 | WP-5: движок интервьюера бессостойный к рестарту — контекст хода всегда из БД (сессия + последние 20 реплик из session_events); LLM — синхронный ход в read-loop (детерминизм + правило единственного писателя), неготовность LLM → стандартная fallback-реплика ai_text (FR-V8), сессия не прерывается; `LLM_MOCK=1` — мок для dev/CI (ADR-005). Голосовая асинхронная оркестрация (VAD+STT+стриминг TTS) — WP-4/8 |
| 29 | 2026-09-14 | WP-4: STT faster-whisper (lazy load singleton+lock, CPU int8, VAD-фильтр Silero-onnx против галлюцинаций, принимает raw PCM16 и WAV, молчание → text=""); TTS Silero v5 (официальный torch-пакет v5_ru с models.silero.ai — pypi-обёртка silero 0.5.5 оказалась устаревшей; 5 рус. спикеров; нативные 24 кГц → ресемплинг 24→16 кГц (линейная интерполяция) в контракт; `TTS_SPEAKER` с fallback + warning). `VOICE_STT_PROVIDER`/`VOICE_TTS_PROVIDER`: реальные по умолчанию, `fake` для CI. Bэклог: стриминг TTS по предложениям, GPU-конфиг, кэш моделей в docker-образ |
| 30 | 2026-09-14 | WP-4/WP-6: окружение без pip/ensurepip — venv создаётся `python3 -m venv --without-pip` + get-pip.py; torch ставится с CPU-индекса pytorch.org/whl/cpu (иначе nvidia-* ~2GB); make install учитывает (Makefile, только voice-цель) |
| 31 | 2026-09-14 | Голосовой конвейер (ADR-002) реализован в api: PCM-кадры → энергетический VAD (RMS-порог, тишина-хвост, мин/макс реплики) → voice /stt → движок интервьюера → voice /tts → бинарные кадры {seq,flags LE}+PCM16. Ходовой режим: turn-taking (время TTS/занятость конвейера — микрофон не слушается, barge-in вне скоупа), один параллельный голосовой ход. MVP-упрощение VAD — энергетический (без onnx в Go; точная Silero-VAD-модель — бэклог). Kонтракт кадров зафиксирован: 4-байтный заголовок LE, 250 мс, bit0 flags — конец потока. LLM-ответ и приветствие озвучиваются; сбой voice — warn-лог, текстовый режим жив |

## Ограничения
- Общение с пользователем — на русском.
- Коммит без явного разрешения — запрещён.
- MVP: все стадии (голос, Live-Code, System Design) — для Go + Python (решение #11);
  технически сначала рабочий голосовой конвейер, затем стадии.

## Открытые вопросы (остаток, 2026-09-09)
- **Тарифные цифры поминутной модели** (не определены; в MVP не нужны).
- ~~Определение «1 минуты интервью» для тарификации~~ — **закрыто** (2026-09-09): REQUIREMENTS.md, раздел 7.
- ~~Планка «живого диалога»: latency-бюджет~~ — **закрыто** (ADR-002); barge-in/естественность — бэклог Operations.
- ~~Критерии и веса отчёта по грейдам (Junior…Staff)~~ — **закрыто** (v1.1): REQUIREMENTS.md §12.

## Итог Discovery (phase gate, 2026-09-09)
- **Цель**: веб-сервис ИИ-мок-интервью для разработчиков: живой голосовой диалог с
  ИИ-интервьюером (только русский), стадия Live-Code (редактор + тесты + ИИ-ревьюер),
  стадия System Design (блоки + рисование, ИИ-оценка схемы и ответа), итоговый отчёт
  с рекомендациями.
- **Критерии успеха MVP**: кандидат на Go или Python проходит сессию целиком — живой
  голосовой диалог (цель end-to-end < 2–3 с), решает Live-Code-задачу с запуском тестов,
  рисует схему системы и получает ИИ-оценку, в конце — отчёт с оценками по критериям.
  60 минут бесплатно, поминутный учёт.
- **Ключевые допущения**: Web (браузер); self-hosted голосовой стек
  (faster-whisper large-v3-russian GPU, Silero v5 MIT) за /api/v1/stt, /api/v1/tts;
  LLM — Qwen3.8-27B (Apache 2.0, vLLM, GPU); сандбокс Go/Python — Docker.
- **Ключевые риски**: R7 (планка «живого диалога»), R1 (latency end-to-end),
  R2 (безопасность сандбокса), R8 (определение «минуты интервью»).
- **Phase gate**: утверждено пользователем 2026-09-09 (платформа, голосовой стек, git).

## Лог
- **2026-09-09** — старт проекта. Фаза Discovery:
  - Прочитан существующий прототип лендинга (грейды, стеки, блоки программы) — учтён в контексте.
  - Созданы `AGENTS.md` (правила), `PROJECT_MEMORY.md` (этот файл), `roadmap.md` (фазы SDLC).
  - `ARCHITECTURE.md` v0.1 — черновик архитектуры с Mermaid-диаграммой
    (голосовой конвейер, Live-Code, System Design, backend).
  - Проведён опрос по сбору требований (13 вопросов); ответы получены и зафиксированы
    (решения #4–14 в таблице выше). Открыто: подтверждение платформы, итоговый выбор TTS/STT.
  - Проверена лицензия Qwen3.8-27B: Apache 2.0 (Hugging Face, 2026-08-14) — коммерческое
    использование разрешено; модель dense 27B, контекст 262K, native vision.
  - Подтверждена платформа (Web). Голосовой стек — self-hosted: faster-whisper
    large-v3-russian (GPU) + Silero v5 (MIT); /api/v1/stt, /api/v1/tts + абстракция провайдера.
  - Итог Discovery записан, phase gate пройден. Подключён удалённый репозиторий
    github.com/stelmakhdigital/Grade-IV (ветка master, история с f9d3a4c); первый коммит + push.
  - → Фаза 1: Requirements.
- **2026-09-09** (Фаза 1) — SRS v1.0 (`REQUIREMENTS.md`): скоуп MVP, 10 user stories
  (MoSCoW) с критериями приёмки, FR-A/S/V/C/R/B, NFR-1…10, определение «минуты интервью»
  (R8 закрыт), явный список вне скоупа. SRS одобрена пользователем (phase gate), закоммичена.
  - → Фаза 2: Design.

- **2026-09-09** (Фаза 2) — Design: ADR-001…005 (docs/adr/), ARCHITECTURE.md v0.3 (компоненты,
  модель данных — 7 таблиц, контракты REST/WS/voice/sandbox, 3 sequence-диаграммы mermaid,
  топологии prod/dev, конфиг и метрики), REQUIREMENTS.md v1.1 (§12 — критерии отчёта по грейдам),
  WBS WP-1…WP-12 (roadmap). Уточнение от пользователя: рантайм-цель — отдельные серверы,
  LLM и STT/TTS — на отдельном сервере в ЛВС (AI-узел) → решение #20, ADR-005 и
  ARCHITECTURE.md §2.1/§6 обновлены.
  - Правка от пользователя: бекэнд — Go (api/sandbox) → ADR-006, ARCHITECTURE v0.4, WBS обновлены.
  - Phase gate Design пройден (2026-09-09); фаза Design закоммичена и запушена в origin/master.
  - → Фаза 3: Implementation (бекэнд — Go, ADR-006).

- **2026-09-10** (Фаза 3) — WP-1 + WP-2:
  - WP-1: каркас — services/api (Go: config/db/models/auth/httpapi, log/slog JSON), services/sandbox
    (Go: health + stub /runs 501), services/voice (Python: /api/v1/health + фейки STT/TTS),
    services/frontend (React+Vite+TS: каркас, health-check API, vitest), infra/ (.env.example,
    docker-compose profile prod, 4 Dockerfile), Makefile (install/test/build/run-*/up), .gitignore.
  - WP-2: api (Go) — модель данных: DDL 7 таблиц (sqlite + postgres, go:embed, миграции),
    UserStore (users, minutes_ledger); auth: register/login/me (JWT HS256, bcrypt), middleware.
  - Тесты зелёные: go test (api: auth/db/httpapi), go test (sandbox), pytest (voice),
    vitest (frontend) + tsc/vite build; smoke-тест собранного api (healthz→register→me).
  - ARCHITECTURE.md → v0.4.1 (§6: ADDR/JWT_*/SANDBOX_URL/LOG_LEVEL).
  - WP-1/WP-2 закоммичены (коммит 8194f66).

- **2026-09-13** (Фаза 3) — WP-3: api (Go) — сессии + WS + тарификация:
  - `internal/session`: `state.go` — машина состояний (стадии voice→livecode→[design]→report;
    Junior без design, возврат на 1 стадию назад, report терминальная; статусы
    active↔paused → finished/aborted). `engine.go` — движок: Create (402-проверка минут),
    Pause/Resume/Finish, Transition (валидация по грейду), Detach (обрыв → paused, FR-S7),
    pause > порога → aborted (SRS §7), секундный тик (начисление, авто-финализация по лимиту,
    timer-сообщения 1 раз/5 с, персист active_seconds), рекавери при старте (active → paused),
    биллинг в minutes_ledger (−округлённые активные секунды) при финализации.
  - `internal/db/sessionstore.go` — SessionStore (CRUD сессий, GetOwned, события c seq,
    MarkPaused/MarkActive/Finalize/PersistActiveSeconds, ListByStatus для рекавери).
  - REST (httpapi): POST/GET /api/v1/sessions, GET /sessions/{id}, POST .../pause|resume|finish,
    GET .../events; маппинг ошибок (402 out_of_minutes, 404, 409 invalid_state).
  - WS `/ws/session/{id}` (nhooyr.io/websocket): auth JWT (?token / Bearer, 401 до апгрейда),
    Attach/Detach (обрыв → paused), binaрные PCM-кадры принимаются (конвейер — WP-4/5),
    ui-события: stage_action, finish, code_run_requested/submit_solution (→ code_run),
    whiteboard_saved, utterance (→ user_utterance); S→C: stage (стартовое), timer, error.
  - Middleware: `statusRecorder.Hijack()` — прокидка для WS-апгрейда (интерфейс
    http.ResponseWriter не промует Hijack — ловушка Go).
  - ENV: SESSION_PAUSE_TIMEOUT_S (по умолч. 1800 с) подключён к движку.
  - Тесты зелёные: go test + go test -race (session: state/engine — fake-часы; httpapi:
    REST + WS через httptest), go vet; live-smoke: REST-цикл (register→create→pause/resume/
    finish→events) и WS-цикл (stage→pcm→stage_action→finish→timer/stage report) на собранном
    бинарнике.
  - ARCHITECTURE.md → v0.4.2 (§4.2 auth WS + utterance, §3 kind событий).
  - Среда: Go 1.26.8 установлен в ~/.local/go-toolchain (PATH: $HOME/.local/go-toolchain/bin);
    PATH в новых bash-сессиях не сохраняется — перед go-командами:
    `export PATH=$PATH:$HOME/.local/go-toolchain/bin`.
  - WP-3 закоммичен (`68afcf5`) и отмечен [x] в roadmap (2026-09-13, разрешение получено);
    запушен в origin/master (8194f66..68afcf5). Примечание: ~/.ssh/id_ed25519 защищён
    passphrase — push из этой среды делается через SSH_ASKPASS_REQUIRE=force + временный
    askpass-хелпер (passphrase предоставил пользователь).

- **2026-09-14** (Фаза 3) — WP-6: sandbox (Go) — runner + банк задач + api-прокси /runs:
  - `services/sandbox/internal/tasks`: банк — 12 задач (6 Go: reverse-string, two-sum,
    fizzbuzz, strstr, lru-cache, parallel-sum; 6 Python: palindrome, two-sum, word-count,
    flatten, lru-cache, rate-limiter), теги junior/middle/senior/staff, go:embed, stdlib-only.
    Каждая: стартовый стуб + скрытые тесты; валидация решаемости подтверждена (корректные
    решения проходят, стобы — нет). Исправлен кейс go-two-sum в банке (ошибочные ожидаемые
    индексы).
  - `services/sandbox/internal/runner`: два режима ADR-003 — `subprocess` (dev-дефолт):
    честный интерпретатор хоста, изолированный cwd (вне /tmp — решение #26), минимальный
    env (GOPROXY=off для Go), таймаут 10 с + убийство группы процессов (Setpgid), cap вывода
    1 МБ, валидация путей (no ..); `docker`: `docker run --rm --network=none --cpus=1
    -m 512m --pids-limit=128 --read-only --tmpfs /tmp --user 1000:1000`, образы
    golang:1.24 / python:3.12-slim (в этой среде docker нет — проверяется на docker-узле).
    Result: `{exit_code, stdout, stderr, duration_ms, passed, timeout, tests[]}`;
    Go tests[] — парсинг `go test -json`, Python — единый pytest-вход.
  - HTTP sandbox: POST /api/v1/sessions/{id}/runs `{stack, files, action, task_id?}`
    (400/503 маппинг), GET /api/v1/tasks?stack=&grade=, /healthz {mode, tasks}.
  - api (Go): POST /api/v1/sessions/{id}/runs (requireAuth, только стадия livecode, иначе 409;
    прокси в SANDBOX_URL 15 с) → сохранение в submissions (db.SubmissionStore), событие
    code_run, run_result по WS (engine.SendTo). Роут более специфичный, чем {action}
    (литеральный сегмент — приоритет Go mux).
  - Тесты зелёные: go test + go test -race (sandbox: runner/httptest; api: runs-прокси с
    mock-sandbox, submissions, events). E2E: банк (12/12 стартуют), live-интеграция
    (register→create→WS livecode→runs через реальный sandbox subprocess→events).
  - ARCHITECTURE.md → v0.4.3 (§4.1/§4.4: контракт runs + tasks, режимы, банк, прокси,
    MVP-отклонение).
  - Среда: pytest для dev-subprocess установлен колёсами в ~/.local/pydeps (нет pip в
    системе); PYTHONPATH=$HOME/.local/pydeps при запуске sandbox (раннер прокидывает).

- **2026-09-14** (Фаза 3) — WP-5: api (Go) — LLM-слой + движок интервьюера:
  - `internal/llm`: OpenAI-совместимый клиент (`/chat/completions`, non-streaming MVP),
    `Provider`-интерфейс (ADR-005), `MockProvider` (детерминированное эхо + журнал
    запросов), `ErrLLMUnavailable`, DefaultTimeout 30 с.
  - `internal/interviewer`: бессостойный движок (контекст — из БД): `OnUserUtterance`
    (ответ + событие ai_utterance), `OnStageChanged` (первый вопрос/представление стадии),
    `OnCodeRun` (ревью по run_result + follow-up, только livecode), `Nudge` (ai_nudge).
    Промпты: персона (строго-доброжелательный сеньор, решения #6/#7), фокус по грейду,
    промпт по стадии; транскрипт — последние 20 реплик (user_utterance/ai_utterance/ai_nudge).
  - Оркестрация (ws.go): `utterance` → LLM → `ai_text` (синхронный ход, правило
    единственного писателя); приветствие на подключении к новой сессии; вход livecode —
    задача из банка sandbox (`GET /tasks`, stage с task) + ai_text; вход design — ai_text;
    nudge-loop: тишина > SILENCE_NUDGE_S на активной voice-сессии → nudge (PCM-кадры и
    тексты сдвигают отсчёт); LLM-сбой → FallbackText (FR-V8).
  - runs (WP-6) дополнен: после run_result — ИИ-ревью fire-and-forget → ai_text.
  - Конфиг: SILENCE_NUDGE_S (8 с), LLM_MOCK=1 (мок). `NewWithLLM` — инъекция провайдера.
  - Тесты: llm (httptest OpenAI-мок, ошибки), interviewer (промпты, транскрипт, stage-gate,
    terminal), WS (utterance→ai_text, nudge при молчании, livecode-task из mock-sandbox);
    go vet + go test -race зелёные. Live-smoke (LLM_MOCK + реальный sandbox): старт →
    приветствие → utterance → livecode(task go-fizzbuzz) → runs(run_result+ревью) → события.
  - ARCHITECTURE.md v0.4.4 (§4.2 оркестрация, §6 LLM_MOCK), .env.example — полный набор.

- **2026-09-14** (Фаза 3) — WP-4: voice (Python) — реальные STT/TTS (под-агент, отчёт
  сверен):
  - `app/faster_whisper.py`: STT faster-whisper (default `small`, `STT_MODEL`), lazy load
    (singleton+lock), CPU int8 (`STT_DEVICE`), lang ru, Silero-VAD onnx-фильтр
    (галлюцинации на тишине), raw PCM16 и WAV (RIFF-парсинг, ресемплинг), молчание →
    `text=""`.
  - `app/silero.py`: TTS Silero v5 (`v5_ru`, 5 рус. спикеров) — прямой download официального
    torch-пакета (pypi `silero` 0.5.5 — устаревшая обёртка), lazy load в
    `services/voice/models/`, синтез 24 кГц → PCM16 16 кГц (контракт).
  - `app/main.py`: `build_app(stt,tts)`; POST /api/v1/stt (multipart), POST /api/v1/tts
    (audio/pcm, стрим ~250 мс, X-Sample-Rate/Channels/Bits, 400 на пустом), GET /health
    (provider/model/device/loaded + speakers); выбор провайдеров по env (fake для CI).
  - requirements.txt: faster-whisper, onnxruntime, numpy, scipy, torch (CPU-индекс),
    python-multipart; Makefile install: venv --without-pip + get-pip + torch cpu ДО
    requirements; .gitignore: services/voice/models/.
  - Проверено: pytest 12 passed (включая e2e с реальными моделями: TTS→PCM, тишина→"",
    раундтрип TTS→STT); smoke uvicorn (lazy loaded=false→true; tiny распознал фразу
    conf 0.603; TTS 64КБ PCM). Модели: TTS v5_ru 139M локально, tiny ~75M HF-cache;
    small (~480M) скачается лениво при первом /stt.
  - Bэклог (решение #29): стриминг TTS по предложениям, GPU-конфиг, кэш моделей в образ,
    STT_MODEL=small в CI.

- **2026-09-14** (Фаза 3) — Шаг «голосовой конвейер» (ADR-002) в api (Go):
  - `internal/voicesvc`: HTTP-клиент voice-сервиса: STT (multipart, PCM16 16 кГц,
    молчание → text=""), TTS (PCM в память, MVP), Healthy; таймауты 30/60 с.
  - `internal/vad`: энергетический VAD реплик (RMS-порог `VAD_RMS_THRESHOLD`,
    конец по тишине `VAD_END_SILENCE_MS`, MinSpeechMS 400, MaxSpeechMS 20000 —
    принудительный срез); юнит-тесты (тишина/хвост/всплеск/срез/пауза внутри реплики).
  - Пайплайн (`voice_pipeline.go` + ws.go): бинарные кадры → VAD → goroutine
    STT → `runCandidateTurn` (общий с текстовым `utterance`): событие
    user_utterance + transcript(user) → LLM → transcript(ai) + ai_text →
    streamAIAudio (TTS → кадры {seq,flags LE}+PCM16 через `engine.SendBinary`).
    Приветствие озвучивается. Turn-taking: busy/ttsActive — микрофон не слушается
    (SRS §8), один параллельный ход; nudge-цикл тоже уважает busy/ttsActive.
  - Движок: `SendBinary` (тот же connMu — правило единственного писателя).
  - Конфиг: VAD_END_SILENCE_MS (900), VAD_RMS_THRESHOLD (500).
  - Тесты: voicesvc (httptest-мок voice), vad (6 кейсов), WS-интеграционные:
    полный контур (тон → transcript/ai_text → TTS-кадры, события) + формат
    заголовков кадров (seq монотонен, end-флаг на последнем). go vet + go test
    + go test -race — зелёные.
  - Live-smoke (api + voice uvicorn с fake-провайдерами + mock-LLM): старт →
    приветствие + 2 TTS-кадра → «речь» (синусоидальные кадры) → VAD → STT
    → transcript/user → ИИ → transcript/ai + ai_text + 2 TTS-кадра → события
    user_utterance/ai_utterance. LIVE6 VOICE SMOKE OK.
  - ARCHITECTURE.md v0.4.5 (контракт кадров, пайплайн, VAD_RMS_THRESHOLD).
  - Подводный камень nhooyr: Read с истёкшим ctx **закрывает соединение**
    (timeoutLoop) — тесты/клиенты не делают «висящие» чтения с дедлайном.
  - Закоммичено (6fd8a33) и запушено; roadmap WP-6a → [x] (bfda9bc).

- **2026-09-14** (Фаза 3) — Шаг «WP-7: Frontend — кабинет кандидата» (базовая часть):
  - `src/api.ts`: типизированный API-клиент grade-api: JWT в localStorage
    (grade.token, Bearer), `ApiError{status,code,msg}` по кодам тела,
    register/login/me, create/list/get session, listEvents, apiHealth (/healthz).
  - `src/auth.tsx`: AuthProvider (при старте /auth/me; 401/403 — разлогин;
    сетевой сбой — гость, но токен не удаляется), useAuth (user, minutes).
  - Views: LoginView (вход/регистрация, валидация email+пароль, маппинг
    кодов ошибок), CabinetView (email, минуты, «новое интервью» grade+stack
    с лимитом минут 45/50/60/75, история со статусами: завершённые →
    «Транскрипт», активные → «Продолжить»), TranscriptView (шапка сессии +
    события из /events с подписями labels.ts).
  - App.tsx: hash-роутинг без зависимостей (#/ — кабинет/вход, #/sessions/:id).
  - nginx.conf + vite.config: location/proxy `/healthz` (healthz живёт БЕЗ
    префикса /api — баг WP-1 исправлен).
  - Тесты vitest: 20 (api-клиент 6, labels 2, App 4, LoginView 4, CabinetView 4);
    tsc --noEmit + vite build — зелёные; smoke собранного бандла (статика + api).
  - Зависимости: +@testing-library/user-event (dev); pnpm-workspace.yaml
    allowBuilds.esbuild=true (build-скрипты pnpm 12 по умолчанию запрещены).
  - ARCHITECTURE.md v0.4.6 (SPA-каркас кабинета).
  - Осталось по WP-7: голосовая сессия UI (WP-8) — WebSocket-клиент,
    AudioWorklet-микрофон, PCM-воспроизведение, таймер, транскрипт вживую.

- **2026-09-14** (Фаза 3) — Шаг «WP-8: голосовая сессия (frontend)»:
  - `src/ws.ts`: SessionWS — WS-клиент сессии: /ws/session/{id}?token= (тот же
    JWT), JSON-сообщения (stage/timer/ai_text/transcript/run_result/
    report_ready/error) + бинарные TTS-кадры {seq,flags LE}+PCM16; sendUi
    (stage_action/finish/utterance), sendPcm (микрофон), close 1011 → aborted.
  - `src/audio/resample.ts` (чистый, юнит-тесты): float32→int16 16 кГц
    (линейная интерполяция, сатурация), parseTtsFrame (заголовок {seq,flags}),
    pcmDurationS.
  - `src/audio/mic.ts`: MicCapture — getUserMedia → AudioWorklet (blob-скрипт,
    ресэмплинг до 16 кГц при любой системной частоте) → чанки PCM16 250 мс;
    заземление gain 0 (звук в динамики не идёт); статы idle/running/denied.
  - `src/audio/player.ts`: PcmPlayer — очередь AudioBuffer (AudioContext 16 кГц),
    планирование по времени, isSpeaking() — индикатор «ИИ говорит» (SRS §8).
  - `SessionView` (заменила TranscriptView): активные/паузные — голосовой экран
    (соединение, таймер mm:ss, живой транскрипт, подписи ai_text, микрофон,
    «К Live-Code»/«К System Design» → stage_action, «Завершить интервью» →
    finish, error/aborted); завершённые — запись из /events. Live-Code/
    System Design — плейсхолдеры (WP-9/WP-10), задача стадии показывается
    из stage.task.
  - Тесты: +resample (7), +ws (6, fake WebSocket), +SessionView (4: запись,
    таймер/транскрипт/finish, denied-mic, stage_action) — итого 37; tsc +
    vitest (стабильно ×2) + vite build — зелёные.
  - Ограничения MVP: без реконнекта WS (detach → paused, переподключение —
    новая вкладка/кнопка «Продолжить»), без barge-in (turn-taking серверный),
    без эхо-подавления (AEC — бэклог), livecode/design UI — плейсхолдеры.
  - ARCHITECTURE.md v0.4.7.
  - WP-7 (кабинет) закоммичен (e5af092) и запушен; roadmap: 3199d7d.

- **2026-09-14** (Фаза 3) — Шаг «WP-9: Live-Code (frontend)» + api (файлы задачи в WS):
  - api (ws.go): `stage.task` теперь несёт `files` (полный набор файлов задачи
    из банка sandbox, включая тесты) — кандидат сдаёт их обратно в /runs.
  - `src/ws.ts`: StageTask.files; `api.ts`: runTests() + RunResult/RunTest.
  - `CodeEditor.tsx`: Monaco (@monaco-editor/react, динамическая загрузка —
    бандл кабинета остаётся лёгким).
  - `LiveCodePanel.tsx`: вкладки файлов задачи (solution.go активен —
    solutionFile()), Monaco, «Запустить тесты» → POST /runs (все файлы +
    task_id), вывод: тесты ✓/✗ (+output), stdout/stderr, «Тесты: n/m · ms»,
    ИИ-ревью (last ai_text на стадии livecode); без task.files — шаблон
    main.go/main.py. Ошибки sandbox — «Sandbox: …».
  - SessionView: на стадии livecode — панель вместо голосового блока
    (таймер/finish/статусы остаются); run_result → системная строка транскрипта
    + сброс review (ждём свежее ai_text).
  - Тесты: +runTests (api), +LiveCodePanel 6 (дефолт, запуск+вывод, sandbox-error,
    review, python, файлы задачи), +SessionView livecode (задача с files,
    /runs body) — frontend 45; api: go vet + go test — зелёные.
  - e2e live-smoke на бинарниках (api LLM_MOCK + sandbox subprocess):
    create → WS → stage_action livecode → задача go-fizzbuzz (3 файла) →
    решение FizzBuzz → /runs passed=true (go test) → run_result по WS →
    ИИ-ревью → события. LIVE9 LIVECODE SMOKE OK.
  - Подводные камни: движок шлёт stage(task=null) при Transition, затем
    оркестратор — stage с task (UI применяет оба; клиенты ждут task!=null);
    sandbox subprocess ищет go в PATH процесса; task-файлы (в т.ч. тесты)
    клиенту приходят только через stage.task.files.
  - ARCHITECTURE.md v0.4.8.
  - WP-8 закоммичен (09a4af8) и запушен; roadmap: f50548f.

## Next steps
1. Коммит WP-9.
2. WP-10: System Design (Excalidraw + палитра 12 блоков, сохранение, ИИ-оценка).
3. Бэклоги: AEC/эхо-подавление, запрет редактирования тестов задачи, стриминг TTS по предложениям, точная Silero-VAD в Go (onnx), GPU-конфиг, long-lived контейнеры sandbox.
