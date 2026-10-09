# План тестирования «Грейд» (Фаза 4)

**Статус:** v1.0 (2026-09-14) — покрывает MVP (Go+Python, все стадии).
Связанные документы: REQUIREMENTS.md (NFR, SRS §12), ARCHITECTURE.md §6 (конфигурация и наблюдаемость),
AGENTS.md (правила).

## 1. Стратегия (пирамида)

| Уровень | Что | Инструмент | Где |
|---|---|---|---|
| Юнит | чистые модули: VAD, resample, парсер PCM-кадров, банк задач, runner-валидация, стейт-машина, критерии/веса отчёта, api-клиент, панели UI | go test, vitest, pytest | services/*/internal, src/**.test.ts(x), voice tests |
| Интеграция | httpapi (REST+WS) на in-memory sqlite + mock-LLM; sandbox-раннер (subprocess); voice-провайдеры (fake) | go test (httptest, nhooyr websocket), vitest (jsdom) | api/internal/httpapi, sandbox/internal/runner, voice |
| E2E | реальные бинарники: полный контур сессии (REST+WS+sandbox+LLM-мок) | bash/Go-клиенты + `make smoke` | scripts/smoke.sh, /tmp/liveN (голос/livecode/design/отчёт) |
| Load | одновременные WS-сессии, latency голосового контура, /runs | wrk/k6 + Go-load-клиент | Фаза 4, задача «Load» |

**Принципы:** детерминизм (mock-LLM, fake-провайдеры STT/TTS — CI без ML-моделей);
каждый контракт (REST/WS) покрыт тестом-контрактным; e2e — на бинарниках,
не на dev-серверах; флейк = баг (стабильность: 3+ прогона до коммита).

## 2. Матрица «требование → тест»

### 2.1. Кабинет и аутентификация (FR-A)
| Требование | Тесты |
|---|---|
| Регистрация/логин/JWT (168 ч), 401 на просроченном | api: auth_test (register/login/expire), frontend: LoginView/App.test |
| Минуты: грант 60, списание по finish, ledger | api: billing/ledger-тесты (WP-3) |
| История сессий, статусы | api: sessions list; frontend: CabinetView, App.test |

### 2.2. Сессия и голос (FR-S, FR-V)
| Требование | Тесты |
|---|---|
| Стейт-машина стадий/статусов (включая «по одной стадии вперёд») | session: state_test + engine_test |
| WS-протокол (stage/timer/ai_text/transcript/error, auth 401 до апгрейда) | httpapi: ws_test, interviewer_test (WS-флоу) |
| VAD: конец реплики по тишине (end-silence 900 мс, min 400) | vad: юнит; e2e: live6 (4 silence-кадра при 750 мс хвоста — эталон) |
| Turn-taking (ignore mic при busy/ttsActive), one-parallel-turn | httpapi: voice_test (pipeline) |
| TTS-кадры {seq,flags}+PCM16, graceful degradation (нет voice → текст) | httpapi: voice_test; frontend: audio/parser-тесты |
| Nudge при тишине (SILENCE_NUDGE_S) | interviewer_test (ai_nudge) |
| Пауза > порога → aborted + тарификация | engine_test |
| **Latency E2E (SLO)** | Фаза 4 (задача «голосовой контур»): замеры mic→stt→llm→tts→speaker |

### 2.3. Live-Code (FR-C)
| Требование | Тесты |
|---|---|
| POST /runs (только livecode, 409 иначе; sandbox_error/unavailable) | api: runs_test; sandbox: main_test |
| Банк задач (12, task_id unknown → 400), файлы задачи в stage.task | sandbox: tasks-тесты; e2e live9 |
| Monaco, вкладки, запуск, вывод тестов/stdout/stderr, ИИ-ревью | frontend: LiveCodePanel.test (6), SessionView (livecode) |
| Синхронизация taskFiles после монтирования (баг WP-9) | frontend: SessionView livecode (solution.go) |

### 2.4. System Design (FR-S5)
| Требование | Тесты |
|---|---|
| Палитра 12 блоков (ADR-004), вставка, свободное рисование | frontend: DesignPanel.test (6) |
| PUT /whiteboard (upsert, 400/404/409, структура) | api: whiteboard_test; e2e live10 |
| ИИ-оценка (OnDesignSubmit, рубрика) | e2e live10 (mock); юнит — через mock-LLM |

### 2.5. Отчёт (FR-R, §12)
| Требование | Тесты |
|---|---|
| Критерии/веса по грейдам, пороги рекомендации | interviewer: report_test (LLM-JSON, веса §12: 4.25 → «с запасом») |
| Fallback-эвристика (mock), 202→200, 409 | api: report_test (lifecycle/errors); frontend: ReportView.test (4) |
| UI: шкалы, колонки, polling, «Повторить» | frontend: ReportView.test, SessionView (отчёт) |
| e2e: finish → 202 → 200 | live11, scripts/smoke.sh |

### 2.6. НФР (NFR)
| Требование | Тесты |
|---|---|
| Изоляция sandbox (docker: network=none, лимиты; subprocess: user/timeout) | sandbox: runner_test; Фаза 4 (задача «сандбокс») — пробои |
| Деградация (нет voice/sandbox/LLM — явные коды ошибок) | api: runs_test (sandbox_error), whiteboard/report — 4xx, voice degradation |
| Масштаб (concurrent сессии) | Фаза 4: load-тест |

### 2.7. UI/клиент: надёжность захвата микрофона (ADR-008)

Где искать: **vitest**, `services/frontend/src/audio/mic.test.ts` (+
`mic-silence.test.ts` для `muted`-детектора, решение #34). Переходы цепочки
моделируются без реального аудио (jsdom: фейки `getUserMedia`/AudioContext),
поэтому сценарии детерминированы.

| Сценарий | Ожидаемое поведение |
|---|---|
| Переходы worklet → fallback → повторная инициализация (чанки не идут) | Каждый переход — info-сообщение (`onInfo`, не ошибка); `getUserMedia` вызывается заново на повторной инициализации |
| Первый чанок с любого пути | Счётчик попыток сбрасывается — пересоздания прекращаются, штатный режим |
| Исчерпан лимит повторных инициализаций (именованная константа) | Ошибка в UI с указанием открыть диагностику `/audio-debug.html` |
| Ошибка `getUserMedia` (отказ в разрешении) | Состояние `denied`, ошибка — без цикла ретраитов |
| Мьют: вход «молчит» (rms ≈ 0 ≥ `SILENCE_MS`) | Состояние `muted` + предупреждение с инструкцией открыть `/audio-debug.html`; **пересоздания по тишине НЕ идут** (ретраит только по отсутствию чанков) |
| Восстановление уровня (rms ≥ `SILENCE_RMS` после `muted`) | `onRecover`/состояние `running`, повторный срыв — новое предупреждение |

## 3. Голосовой контур: latency и качество (задача Фазы 4)

**Цели (SRS FR-V4 / ARCH §6):** p95 mic→TTS-старт < 2.5 c (LLM-мок; с LLM — < 4 c),
без перебиваний (turn-taking), STT-качество на русской речи (WER-образная оценка
на эталонном наборе 20 фраз).

**Методика:**
1. Harness (Python, services/voice/scripts/latency_probe.py): генерация эталонных
   PCM-фраз (TTS Silero → эталон), отправка в живой голосовой контур (WS-клиент),
   замер таймстампов: frame-out → user_utterance → ai_utterance → первый TTS-кадр.
2. Метрики: p50/p95 по 30 прогонам; регресс-база (json-отчёт) в docs/test-results/.
3. Качество STT: прогон 20 фраз → сравнение с эталоном (Levenshtein), порог
   сходства ≥ 0.85 (small-модель, CPU) — ниже → эскалация на large-v3/GPU.
4. Fake-провайдеры (CI): детерминированная проверка конвейера без ML.

### 3.1. Clause-level диспетчизация TTS (поправка ADR-002, 2026-10-08)

**Поведение под проверкой:** разрывы между фразами реплики ИИ 750–1500 мс — узкое место
скорость LLM-стрима (предложение уходило на TTS целиком сгенерированным; синтез ~89 мс
на предложение — не bottleneck; решение #36). Решение: предложение делится на клиузы по
«,» «;» «:» «—», ранний запуск TTS клиуза при окне `MIN_CLAUSE_CHARS` (~48 символов),
паузы между частями по завершающему знаку: «?/!» → 500 мс, «.»/«...» → 250 мс,
«,»/«;»/«:»/«—» → 0. Отчёт замеров — docs/test-results/tts-clause-dispatch-2026-10-08.md.

| Сценарий | Тип | Ожидаемое поведение |
|---|---|---|
| Юнит разбиения на клиузы: границы по «,» «;» «:» «—» (граница в конце клиузы, знак входит в клиузу) и по завершителям «.»/«...»/«?»/«!» | Go-тест, детерминированный | Корректный набор клиуз, включая граничные случаи (знак в конце, пустой хвост) |
| Юнит «окно по длине»: клиуза короче `MIN_CLAUSE_CHARS` | Go-тест, детерминированный | Клиуза не уходит на TTS до накопления окна (≥ `MIN_CLAUSE_CHARS`) |
| Юнит правила пауз: завершитель «?/!» → 500 мс, «.»/«...» → 250 мс, «,»/«;»/«:»/«—» → 0 мс | Go-тест, детерминированный | Пауза между частями задаётся по правилу; клиузы одной фразы — подряд без паузы |
| Детерминированный тест раннего запуска TTS: mock-LLM с задержкой на токен | Go-тест (интеграционный, mock-LLM) | Первая клиуза уходит на TTS до конца генерации предложения; порядок кадров и нумерация seq сохранены |
| A/B-проба probe-tts-first: бинарники old (без clause-level) / new (с), по 3 пробы каждого | Live A/B (проба, бинарники) | Метрики: first_tts (до первого TTS-кадра) и разрывы между фразами >500 мс — в new-bin первые кадры раньше, разрывы >500 мс не наблюдаются (цифры — в отчёте) |

## 4. Сандбокс: изоляция и лимиты (задача Фазы 4)

1. **Догонялки:** задача-«бомба» (infinite loop) → timeout (10 c) + timeout=true;
   задача-«память» (alloc 2 ГБ) → OOM-kill (docker) / быстрая смерть от RLIMIT
   (subprocess: MemoryError/exit!=0 для python, SIGXCPU→SIGKILL→124/timeout для
   CPU-цикла) — T-20261009133832: subprocess-режим получил rlimit (аналог
   docker ADR-003): RLIMIT_AS (go 2 ГБ VA / python 512 МБ) + RLIMIT_CPU 10 с
   (sh-proлог `ulimit` перед командой; Go stdlib SysProcAttr без Rlimit —
   фиксация в ADR-003). Тесты: TestSubprocessMemoryLimit, TestSubprocessCPULimit,
   TestBankSubprocessRegression (12 задач банка не сломаны rlimit).
2. **Сеть (docker):** контейнер без сети (curl 8.8.8.8 → отказ) — network=none.
3. **FS:** работа только в workdir (создание файлов за пределами → запрет).
4. **Доверенность тестов:** тесты из банка кандидатам видны (MVP-ограничение,
   задокументировано); проверка: тест не может «сдавать» задачу сам
   (задача с тестом-трояном → runner не исполняет произвольные main-запросы).
5. **Docker-пробы (CI + локально, T-20261009133832):** `TestSandboxDockerProbe`
   (opt-in `SANDBOX_DOCKER=1`, skip без docker): go-pass + proof network=none
   (внешний dial обязан упасть), py-pass, py-oom (2 ГБ при --memory=512m →
   OOM-kill 137, без timeout) — Result-контракт (passed/timeout/exit_code).
   CI: джоб `sandbox-docker` (.github/workflows/ci.yml, ubuntu-latest с docker,
   pull golang:1.24 / python:3.12-slim); локально: `bash scripts/ci-docker-test.sh`
   (в среде без docker — exit 2; docker-часть валидируется только в CI).
6. Результаты: docs/test-results/sandbox-{date}.md.

## 5. Load-тест real-time (задача Фазы 4)

**Цель:** 50 одновременных WS-голосовых сессий (LLM-мок, voice fake) —
p95 tick-таймера ≤ 1 c, CPU api < 2 ядра, без потерь бинарных кадров.

**Методика:** Go-load-клиент (scripts/load_ws.go): N соединений, генерация
PCM-чанков 250 мс по расписанию, приём TTS-кадров; метрики: ошибки, latency
timer/ai_text, RSS/CPU (psutil-аналог через /proc). Целевые значения —
ARCHITECTURE.md §6; отчёт — docs/test-results/load-{date}.md.
Инструменты: Go-клиент (основной), k6 — для REST (регистрация/сессии).

## 6. Регрессия и критерии выхода Фазы 4

- **Регресс-прогон (обязателен перед закрытием фазы):**
  `make test` (go vet+test, vitest, pytest — все сервисы) × 3 прогона,
  `make build`, `make smoke`, e2e liveN (voice/livecode/design/report) — все зелёные.
- **Критерии выхода:** все задачи Фазы 4 закрыты; баги из тестов исправлены
  (или приняты пользователем как бэклог с ADR); отчёт в PROJECT_MEMORY.md;
  флейков нет (3 стабильных прогона); одобрение пользователя (phase gate).

## 7. Открытые вопросы

1. GPU-узел для load-теста с реальным STT: доступность в окне Фазы 4
   (иначе — CPU small-модель + отметка в отчёте).
2. Эталонный набор речевых фраз (20 шт.) — подготовить тексты (фаза 4, шаг 1).
3. К8s-опция вместо compose для load: не требуется для MVP (1 узел).
