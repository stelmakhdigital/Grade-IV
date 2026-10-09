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
| 36 | 2026-10-06 | Параллельный TTS-синтез (pipeline, очередь #3; f2aa87e/94d45e9, T-20261005184303 R1): пул TTSParallelism=3 (stdlib goroutine+channel) в streamCandidateTurn — produceOrderedTTS: диспетчер (idx/gap) + TTSParallelism воркеров + записчик с реордером по idx. Предложение N+1/N+2 синтезируется, пока N уходит по pacing; порядок кадров в WS сохранён (тише-паузы 1/2 как раньше). Barge-in: недопущенные предложения не стартуют, идущие завершаются, результаты сливаются (без утечки горутины); TTS-ошибка — skip (warn-лог), остальные синтезируются. Метрика grade_ai_tts_synth_seconds (histogram, реестр all) — длительность синтеза на предложение (обсерв на успех). Тест TestTTSParallelSynthesis: mock TTS с задержкой 700 мс ≥ pacing-окна + амплитудный маркер на предложение — s2-s1=388 мкс << delay (перекрытие), кадры A1 полностью предшествуют A2 (порядок). Факт: nhooyr conn.Read(ctx) закрывает соединение при истечении ctx — read-таймаут теста 2 с > паузы синтеза. Live A/B (тот же 3-с промпт, старый/новый бинарник): first_tts 388/435/730→367/359/433 мс (в пределах LLM-шума), межфразовые разрывы ~без изменений (750–1500 мс) — узкое место LLM-стриминг, синтез ~89 мс/предложение (21 предл., метрика) — TTS не bottleneck; выигрыш проявляется, когда синтез медленнее подачи LLM |
| 35 | 2026-10-05 | Стриминговый STT (ADR-007; ab0bc3f/6556b72/e00b4c6/abb57f6, T-20261005132522 R1): граница реплики — Silero VAD (onnx из бандла faster-whisper, per-connection clone; fallback EnergyVAD) на стороне voice-сервиса — WS /api/v1/stt/stream: клиент шлёт PCM16-кадры, сервер — state/partial (≤1/500 мс, pre-roll 1 кадр)/final (text+confidence+speech_ms). Именованные константы: VAD_THRESHOLD=0.5, VAD_MIN_SILENCE_MS=600, VAD_MIN_SPEECH_MS=250. API: feedVAD → стрим (реконнект ≤4×500 мс), partial → WS stt_partial (интерим-строка кандидата в UI, final подменяет), final → ход кандидата (conf-фильтр 0.5). Barge-in теперь и во время речи: state speech=true + ≥BargeInMinSpeechMS (500 мс) без тишевой границы → таймер прерывает TTS (CAS sttBarged, reset в beginTTS); догон по final (speech_ms). Деградация при недоступности стрима — batch-путь (энергетический VAD + pre-STT + /stt), Warn однократно, метрика grade_stt_stream_fallbacks_total. Live: транскрипт 3-с реплики 754→695 мс (A/B той же репликой на старом/новом коде, git worktree), partial за 2.1 с до конца речи, ложных final на 10-с тишине нет (2 пробы), barge-in +500 мс от начала речи (metric=1, log Info). UI: скриншоты screenshots/stt-streaming/. Баг: CHUNK в latency-пробах был 4000 Б (125 мс) вместо 8000 (250 мс) — «baseline 2004 мс» мерялся на 1.5-с реплике, 0.5× скорости |
| 34 | 2026-10-05 | Диагностика «молчащего» микрофона (881eaae, T-20261005121104 R1; симптом — сессия 13: кадры 2730 Б ScriptProcessor-fallback, rms 2→0, пользователь не слышен). SilenceDetector в mic.ts: rms чанка < SILENCE_RMS=0.005 непрерывно ≥ SILENCE_MS=5000 мс → onState('muted') (новый MicState) + onError «Микрофон молчит: проверьте устройство, мьют и разрешения браузера» (один раз за период); при возвращении уровня — onState('running'), повторный срыв — новое предупреждение; любой путь захвата (worklet/fallback). Fallback-переход — info-сообщение onInfo (не ошибка). UI: индикатор/строка «Микрофон молчит…», toggle остановка в состоянии muted. Корень «не слышен» (мёртвый мик + молчаливый fallback) стал видимым. Live: мьютный вход → warning ~5–8 с; вход с реальным аудио → без ложных срабатываний, STT conf 0.845/0.93 |
| 33 | 2026-10-05 | Full-duplex barge-in (a3bfc11, T-20261005093230 R1): VAD слушает кандидата и во время речи ИИ (ttsActive); порог barge-in — именованная константа BargeInMinSpeechMS=500 мс речи (краткие всплески не прерывают); barge-in → stopTTS (ttsStop), WS {type:tts_stop}, end-кадр прерванного стрима, метрика grade_barge_ins_total, log Info. runCandidateTurn — асинхронно (readLoop не блокируется — иначе реплика не читалась бы во время речи ИИ). Фикс paceFrames: hot-spin на закрытом канале и end-флаг на чужом кадре. Frontend: AEC (echoCancellation+noiseSuppression в getUserMedia), player.stop() останавливает запланированные источники, PCM — всегда (без гейта «ИИ говорит»). Live-замер: tts_stop мгновенно, end-кадр, метрика=1, ложных срабатываний нет |
| 32 | 2026-10-05 | Локальный voice-контур (pipeline T-20261005080715 R1): STT — faster-whisper large-v3 на GPU (RTX 5070 Ti, cuda/float16), TTS — Silero v5 на CPU. Модели — в `services/voice/models/{stt,tts}` (gitignore; run-all.sh указывает туда по умолчанию, STT_MODEL=large-v3, STT_DEVICE=cuda, STT_COMPUTE_TYPE=float16). ctranslate2 требует внешние CUDA 12-библиотеки: `nvidia-cublas-cu12` + `nvidia-cudnn-cu12` в voice-venv (PyPI), run-all.sh собирает LD_LIBRARY_PATH из `site-packages/nvidia/*/lib`. LLM — `http://192.168.1.114:8000/v1` / `qwen3.8-27b-fp8` (дефолты run-all). Замер: STT 5 с аудио = 0.2 с (GPU) vs ~2.5–3 с (CPU int8); сходство TTS→STT 0.846 (3 фразы); LLM-отчёт ~18 с |

| 37 | 2026-10-08 | Пауза/возобновление сессии — полный контур (pipeline T-20261008154023 R1): backend — реестр живых WS-соединений в Server (wsRegister/wsUnregister), pause → stopTTSOnPause (barge-in-механизм: tts_stop + end-кадр, воркеры TTS-синтеза не выпускают новые кадры); статус хода берётся из движка (in-memory Snapshot, атомарно с паузой) вместо чтения БД (гонка «ход стартует в уже ставящейся паузе» закрыта); paused-сессия не гонит PCM в VAD/STT (feedVAD guard) и игнорирует текстовые/голосовые реплики; resume при паузе > порога → 409 invalid_state + aborted. Frontend — pauseSession/resumeSession (POST /sessions/{id}/pause\|resume), кнопки «Пауза»/«Продолжить» + баннер «время не тарифицируется»; при паузе mic.stop + player.stop ДО REST-вызова; при resume автовозврат микрофона (клик = user gesture), ошибка старта мика не ломает сессию; paused — локальный флаг + session.status (переживает перезагрузку); WS на паузе не переподключается. Тесты: session_pause_test.go (5 сценариев), vitest +4 (73). Скриншоты screenshots/session-pause/ (live-стек, LLM_MOCK). Гонка beginTTS-окна закрыта в раунде 2 (T-20261008154023 R2: session-level стоп-канал) |
| 38 | 2026-10-08 | Надёжность захвата микрофона (ADR-008, T-20261008175905 R1): цепочка worklet → (3 с без чанков) → fallback → (3 с без чанков) → полная повторная инициализация (disposeCapture: tracks/context/nodes, getUserMedia заново → worklet), MAX_REINIT=2, NO_DATA_MS=3000; счётчик сбрасывается первым чанком; onInfo «повторяю инициализацию (попытка N)», лимит → onError с /audio-debug.html; getUserMedia-отказ → 'denied' + onError. Самодиагностика: muted (rms < SILENCE_RMS ≥ SILENCE_MS) — сообщение с кликабельной ссылкой /audio-debug.html (строка ошибки + muted-баннер). Ретраит по тишине уровня НЕ делается (браузер не различает «мёртвый путь» и реальную тишину). Тесты: +8 (91/91). Скриншоты screenshots/mic-retry/ (headless, fake-мик + dev-хук __gradeMic). Latency: серверный путь не тронут — проба STT-стрима до/после: final 3.86→3.99 с (в пределах шума), docs/test-results/mic-capture-reliability-2026-10-08.md. Выбор задачи: №1 (Go-VAD onnx) — низкий эффект (основной стриминговый путь уже Silero VAD на voice-сервисе, ADR-007; Go-энергетический VAD — только batch-fallback) + тяжёлая onnx-зависимость в Go; №3 (TTS-разрывы 750–1500 мс) — узкое место LLM-стриминг (решение #36), клиентские приёмы ограничены + нужен A/B на LLM-узле — остаётся в бэклоге |
| 39 | 2026-10-08 | Клиуза-уровневая диспетчизация TTS (T-20261008185701, приоритетная задача 3 из бэклога #38): voice_pipeline.go — MIN_CLAUSE_CHARS=48 (байты), splitForTTSStreaming/splitClausesWithTail (клиузы по «,» «;» «:» «—» для частей ≥ 48; граница — в конце клиузы, «:» в числах не дробит, «—»-фрагменты не уходят в TTS), открытый хвост raw (без обрезки — иначе токены склеиваются), splitDeltas диспетчизует клиузы; ttsGap(prev): «?/!» → 2 (500 мс), «.»/«...» → 1 (250 мс), «,»/«;»/«:»/«—» → 0 (клиузы одной фразы подряд) — в produceOrderedTTS и streamAIAudio. llm/mock.go: SetTokenDelay/StreamEnd (тесты). TestClauseDispatchEarlyTTS: клиуз уходит в TTS за 1.0 с до конца LLM-стрима, gap=0 между клиузами, gap=1 после «.» — зелёный; все тесты api зелёные, go vet/gofmt OK. A/B (live, 3 рана/бинарник): first_tts 1699→1638 мс (−4%, в пределах шума), разрывы 19500→19250 мс — без регрессии; live-эффект маскируется недетерминизмом LLM, механизм доказан детерминированным тестом. docs/test-results/tts-clause-dispatch-2026-10-08.md. WS-контракт/REST не менялись, внешних зависимостей нет |
| 40 | 2026-10-09 | VAD: Silero (onnx) вместо энергетического в batch-пути (T-20261008211334 R1; поправка ADR-002 — вариант 2, merge dee864b). **Выбор варианта:** 1 (onnx-инференс в Go) отклонён — go-onnx-биндинги = cgo → ломает обязательное `CGO_ENABLED=0` (Makefile/CI/Dockerfile), статичность/портативность бинарника; 2 (voice-сервис) выбран — Silero onnx уже живёт в voice (Python), без новых Go-зависимостей, `CGO_ENABLED=0` сохранён; 3 (Go держит state-машину, голосует на voice каждое окно) — ×N сетевых хоппов → отклонён. **Механизм:** voice `WS /api/v1/vad/stream` (Silero VAD, per-connection clone; state-события start / pre_silence / end, без STT/partial) + `StreamVAD.pre_silence_ms` (VAD_PRE_SILENCE_MS=400, дефолт None — `/stt/stream` без изменений; pre_end ровно раз за реплику). api — `voicesvc.VADStream` (тонкий клиент, reconnect ≤3×500 мс → unavailable; локальный буфер: pre-ring ≤4 кадра + буфер речи; после pre_silence тише-кадры RMS<100 не буферизуются → буфер==снимок → pre-STT валиден, возобновлённая речь расширяет → инвалидация); `feedVAD` — диспетчер 3 уровня: `/stt/stream` → `/vad/stream`+batch `/stt` → energy VAD (last-resort, **не удалён**); DRY-хелперы `preSTTFor`/`completeUtterance` (используются energy- и Silero-путями, поведение energy не изменилось); `sileroSpeech`/`isSpeaking()` — анти-nudge по batch-путям; метрика `grade_vad_stream_fallbacks_total`. **Замер** (реальный Silero onnx, фикстуры noise/breathing/silence/quiet/honest speech): ложные реплики energy 1 сег/3008 мс → silero 0 (noise, breathing); честная/тихая речь оба ловят (регрессии нет, quiet 2884/3008 мс); latency конца 908→608 мс (порог 600 против 900 мс). CGO_ENABLED=0 build ок (22.9 МБ, без изменений). Тесты: voicesvc VADStream, httpapi VADStream (pre-STT/barge-in/fallback на energy), voice test_vad_stream.py (10, вкл. реальный onnx). gen_fixtures самодостаточен (речь из закоммиченной фикстуры, FOR_RUN — только fallback первого генерирования). docs/test-results/vad-silero-batch-2026-10-09.md. **Риск:** pre-roll (≤1 с) входит в barge-in-длительность по байтам — следить по grade_barge_ins_total; пороги Silero (0.5/600/250/400) — именованные константы, один файл |
| 41 | 2026-10-09 | Детерминированный A/B-замер голосового пайплайна (T-20261009001516 R1) — закрыт открытый вопрос о влиянии clause-dispatch (#39) на p95. **Метод:** фиксированный LLM-мок (`LLM_MOCK_RESPONSE` + `LLM_MOCK_TOKENS_PER_S=30` — новый env, `mock.SetTokenDelay`, commit efe318c) устраняет LLM-недетерминизм; детерминированный пробер `services/voice/latency/probe_deterministic.py` (фикс. TTS-реплика кандидата 1.95 с в реальном времени, отсчёт «конец речи → первый звук ИИ» first_tts с декомпозицией stt/llm-tts + межфразовые паузы, N=8, p50/p95, JSON). **A/B (BEFORE 4a6e048 + env-mock / AFTER HEAD, общий voice large-v3 CUDA):** first_tts p50 1.46→1.23 с (−0.23), p95 1.65→1.37 с (−0.28); чистый эффект clause-dispatch (llm/tts, транскрипт→1-й кадр) 0.59→0.15 с (−0.44). **SLO p95<4 с (пайплайн-часть) выполняется** (AFTER p95=1.37 с). **Выводы:** clause-dispatch реально ускоряет первый звук (−0.23/−0.28 с); второй узел — STT (VAD-хвост 600 мс + large-v3 ~0.2–0.4 с), доминирует в first_tts и НЕ затрагивается clause-dispatch (дальше быстрее STT/partial-триггер); trade-off turn_end +1.37 с (7 клиузных пауз vs 3 фразовых — более естественный ритм). **Silero VAD batch (#40)** — fallback-путь, в этом A/B не exercised (обе версии — основной streaming STT); его latency (конец 908→608 мс) измерен в решении #40. docs/test-results/latency-deterministic-2026-10-09.md. Ограничения: N=8 (p95 грубое), общий voice (stt-шум ~0.2 с common-mode), LLM=mock (не реальная Qwen) |
| 42 | 2026-10-09 | Barge-in: pre-roll-учёт в пороге + наблюдаемость (T-20261009021739 R1; ADR-002 поправка; коммиты cb7e542/9ba4cc4/75bfe31). **Риск #40 закрыт наблюдаемостью + вариантом (b):** pre-roll (до 1 с тишины в pre-ring VADStream) больше не входит в barge-in-порог — `VADEvent.PreRollMS` (байты pre-ring при старте речи), `completeUtterance(ws, utterance, prerollMS)`: `ms = totalMS − prerollMS` против `BargeInMinSpeechMS=500` (порог не тронут); energy-путь `prerollMS=0` (без изменений). **Метрика** `grade_barge_in_speech_ms` (histogram, мс, бакеты 100/250/500/1000/2000/5000) — обсерв в 3 точках barge-in; наблюдение: p50 устойчиво 500–1000 мс / доля le=500 > 0 — pre-roll-риск. **Grafana v2:** row «Barge-in & Fallback» (rate/мин, доля ≤500 мс, fallback-счётчики, speech_ms p50/p95), «Latency» (+grade_ai_tts_synth_seconds), «Health»; алертные пороги — OPERATIONS §2. **Known limitations (задокументированы):** race кадр-триггера (клиентский ring «догоняет» события: триггер и в burst до 3 кадров считаются pre-roll → речь занижается ≤750 мс, направление безопасное — меньше ложных barge-in); post-silence ≤ ~400–500 мс входит в буфер и НЕ вычитается (ms завышается); точная оценка речи по batch-пути — ±(race+post-silence), точно — speech_ms voice (стрим-путь). Тест TestBargeInSpeechMSBuckets (Silero-путь, детерминизм: приминг-кадр + ожидание деградации по дельте метрики, кадры 50 мс): ms=1250 ∈ (1000,2000]. Регресс: TestBargeIn_*/TestVADStream_* зелёные; полный сьют зелёный. Docker недоступен — monitoring-профиль не поднимался (JSON валиден, имена метрик — по живому /metrics) |
| 43 | 2026-10-09 | A/B промптов ИИ-интервьюера (T-20261009121144 R1; docs/test-results/prompts-ab-2026-10-09.md). **Выбор B:** voice-формат-блок (1–2 предложения, ≤ 40 слов; реакция + один вопрос/подсказка; без списков/«давайте разберём») — `activeVoiceStyle=voiceStyleB`, константы A/B + внутренний билдер `systemPrompt(grade, stack, stage, voiceStyle)`. **Оценка (2 слоя):** (a) детерминированно — промпт-контракт (B содержит ограничение/структуру/запреты, A нет; персона/программы/LiveCode/Design не тронуты) + эвристика реплик (≤40 слов, списки, «т.д.», «?»); (b) LLM-judge (qwen3.8-27b-fp8, 4 критерия 1–5) на живых LLM-ответах, корпус 10 сценариев (7 voice A/B + 3 контекст A=B): **A=18.1, B=18.9**, B не хуже ни в одном, строго лучше в 3 junior (A: 41–62 слова → B: 11–24, grade_fit 3→5). **Latency (probe_deterministic.py, реальный LLM, N=5, бинари A=ddf757c vs B):** first_tts p50 2.921 vs 2.393, p95 3.268 vs 2.494 — **нейтрально** (разница в пределах LLM-шума; +60 токенов prefill). Первый прогон B отброшен (узел LLM кратковременно не отвечал — 3× timeout в логах). Тесты: TestPromptABContract/TestVoiceReplyHeuristic/TestPromptSmokeMock/TestCodeRunHintContract (детерминированные) + TestABEvalLLM (opt-in `LLM_EVAL=1`, vLLM: `enable_thinking=false` как в проде — иначе thinking-фаза съедает max_tokens). Ограничения: N=1 на сценарий/вариант; judge=генератор (состыление стилей); design_review (152 слова, voice_fit=2) — кандидат на отдельное улучшение формата |
| 44 | 2026-10-09 | Prod-гигиена: sandbox-изоляция, CI docker-пробы, prod-compose (T-20261009133832 R1; ADR-003 поправка). **RLIMIT subprocess-раннера** (dev-режим, аналог docker ADR-003): RLIMIT_AS (go 2048 МБ VA — холодная сборка std в per-run GOCACHE требует >1.5 ГБ VA, измерено; python 512 МБ ≈ docker --memory=512m) + RLIMIT_CPU 10 с; механизм — sh-proлог `ulimit -v <KB>; ulimit -t <с>; <команда>` (Go stdlib `syscall.SysProcAttr` **не имеет Rlimit** — только x/sys/unix, несовместимый с os/exec; ulimit-пролог — без зависимости, лимиты наследуются всем дочерним). Маркировка: 128+SIGXCPU / 137 (SIGKILL ядра при превышении CPU-лимита >10%; CPython перехватывает SIGXCPU и продолжает) / SIGXCPU напрямую → 124/timeout. Known limitation: 137 = и os.kill(self,SIGKILL) кандидата; VA≠RSS (RLIMIT_RSS не поддерживается) — dev-защита от «виснет/OOM-хост», точный RSS-бюджет — только docker. Тесты: TestSubprocessMemoryLimit (2 ГБ → MemoryError 0.4 с), TestSubprocessCPULimit (цикл, CPU 2 с → ~2 с, 124), TestBankSubprocessRegression (12 задач банка не сломаны). **CI docker-пробы:** джоб sandbox-docker (ubuntu-latest с docker, pull golang:1.24/python:3.12-slim) + TestSandboxDockerProbe (opt-in SANDBOX_DOCKER=1, skip без docker): go-pass + proof network=none (внешний dial падает), py-pass, py-oom (2 ГБ → 137, без timeout) — Result-контракт; локально scripts/ci-docker-test.sh (exit 2 без docker). **Prod-compose — исправленные рассинхроны:** (1) sandbox.Dockerfile без docker-cli → prod SANDBOX_MODE=docker всегда 503 (compose монтирует сокет, CLI не было) — `apk add docker-cli`; (2) Caddyfile `tls {$CADDY_TLS:letsencrypt}` — невалидный аргумент (пусто|internal|email|cert+key) → caddy не стартовал при пустом CADDY_TLS (prod-дефолт) — `tls {$CADDY_TLS:}`; (3) .env.example: +VITE_MIC_DEBUG (build-время, prod-сборка без него), +STT_DOWNLOAD_ROOT, −STT_COMPUTE_TYPE (код не читает); (4) compose voice +TTS_SPEAKER (дефолт ru_01). Docker в рабочей среде недоступен — валидация структуры (YAML, env/порты/объёмы по коду); запуск контейнеров — при деплое (OPERATIONS §1 чек-лист). Полный сьют зелёный: go vet+test api/sandbox, gofmt -l, tsc, vitest 91/91, vite build, pytest voice 27 |
| 2026-10-09 | Шлифовка non-blocking (T-20261009152316 R1): (1) barge-in metric + result
| 2026-10-09 | Voice UX (T-20261009164629 R1; ADR-009, FR-S8): (1) mic toggle = mute
  (init один раз, < 10 мс, track-ended → reinit, dispose на pause/unmount);
  (2) история — новые сверху (live/interim/REST) + автоскролл к началу;
  (3) режим записи: WS recording on/off + stt_segment, подавление AI-хода
  (stream/batch, после conf-фильтра), nudge-guard, окно записи (текст/мм:сс/
  статус, сворачивание), mic off → одно utterance со склеенным текстом,
  «недостаточно речи» < 1,5 с. Live: 65,9 с (TTS 10 фраз) → 9 сегментов,
  0 ходов, 1 ответ ИИ (voice-recording-2026-10-09.md). Полный сьют зелёный
  (vitest 110/110). Бэклог: 30-с лимит непрерывной речи. Push — не выполнялся. |
| 2026-10-09 | Voice UX — верификация + инцидент stale-бинарника (решение #47): api в работе — старое (без FR-S8) → «неизвестное событие: recording» + авто-ответы на неполных репликах (корень: go build -o на существующий файл не переписывает — кэш линкера). Фиксы: run-all.sh (сборка в tmp + mv + mtime-check), TestRecordingUIEventsNoUnknown, UI: «Отправить»/«Ожидание ИИ…»/«Очистить буфер». Замеры: 65,9 с записи → 9 сегментов, 0 ходов во время, 1 ответ после; краевой 1924 мс → 1/0/1; guard «недостаточно речи». Скриншоты docs/test-results/voice-ux-verify-2026-10-09/. Живая проверка на реальном микрофоне — пользователю.  label (barge_in/ignored), негативный Silero-кейс deterministic (totalMS=800,
  pre-roll=500 → ms=300, без tts_stop, le=500), Grafana 107 + фильтры, OPERATIONS;
  (2) LLM N=3 «ничьих» (LLM_EVAL_RUNS/SCENARIOS): B подтверждён (A=19.2 B=19.5,
  строго лучше algorithms/nudge); (3) designReviewMessage: формат 3-5 предложений
  без списков (TTS), контракт-тест, LLM 163-209 → 95-102 слова; (4) NOTE dead
  agents. Полный сьют зелёный. Push — не выполнялся. |
| 45 | 2026-10-09 | Шлифовка: non-blocking judge-комментарии трёх задач (T-20261009152316 R1). **(1) Негативный barge-in Silero-пути (deterministic):** `grade_barge_in_speech_ms` теперь несёт и непрерванные короткие реплики — метка `result` (`barge_in` / `ignored`): короткий speech (< 500 мс после вычитания pre-roll) → observation с `result="ignored"`, без tts_stop (batch completeUtterance + stream final; бач-таймер — без данных, как было). Тест TestBargeInSileroNegativeShortSpeech: прямой вызов completeUtterance (mock Silero-клиент, 25 600 байт = 800 мс totalMS, preroll=500 → ms=300 < 500): tts_stop=0, le=500 +1, le=750 +1, le=1000 +1 (result=ignored), le=1000 (result=barge_in)=0. Grafana: фильтры result="barge_in" на панелях 101-104 + новая панель 107 «ignored /мин» (эхо/короткие реплики — наблюдаемость pre-roll-вычитания). OPERATIONS: строка ignored в алерт-таблице. **(2) LLM A/B N=3 «ничьих»:** TestABEvalLLM — LLM_EVAL_RUNS / LLM_EVAL_SCENARIOS (подмножество), ретраи + skip (узел нестабильно), усреднение по runs: middle_followup 20/20 (tie), middle_algorithms 19→**20**, senior_challenge 20/20 (tie), senior_nudge 17.67→**18.00**; среднее A=19.2 B=19.5. **Победитель B подтверждён** (не хуже ни в одном, строго лучше в 2; реплики B 9–22 слова vs 21–59 у A) — переключение не требуется, activeVoiceStyle=voiceStyleB не тронут. Дополнение в prompts-ab-2026-10-09.md. **(3) design_review формат:** ограничение НЕ применялось к design-стадии (блок — только voice) — добавлено в user-сообщение OnDesignSubmit (вынесено в `designReviewMessage()`: 3–5 предложений, без списков, «будет озвучен TTS»); контракт TestDesignReviewMessageContract; LLM-проверка (192.168.1.114): без ограничения 163–209 слов (с «Давай разберем»), с ограничением 95–102 без списков. Known: 95–102 слова — не идеально «3–5 предложений», ограничение смягчает; полный промпт-инжиниринг — backlog. **(4)** мёртвые pipeline-агенты (w/p/j-grade) — NOTE для conductor (.pi/pipeline/NOTE-dead-agents.md), механизм удаления в инструментах нет. Полный сьют зелёный |
| 46 | 2026-10-09 | Voice UX: режим записи, микрофон без переинициализации, последние сверху (T-20261009164629 R1; ADR-009, FR-S8). **(1) MicCapture toggle = mute:** `stop()` — soft (track.enabled=false, поток/контекст живут, счётчики отладки сброс, нулевые чанки не шлются), `start()` по живому потоку — enabled=true < 10 мс (без getUserMedia), мёртвый track (readyState ended / событие ended) — полная реинициализация (цепочка ADR-008 + MAX_REINIT), `dispose()` — жёсткое освобождение (pause/unmount). Тесты: getUserMedia ровно 1 раз при toggle, < 10 мс, ended → reinit, ADR-008-регресс. **(2) История — новые сверху:** lines хранятся append (кап 200), рендер перевёрнутый (live/interim/REST — единый порядок), автоскролл контейнера к началу (userNearTop < 80 px), max-height 420 px + overflow. Тесты: порядок (последнее — первый li, interim сверху), REST-история, автоскролл (у края — scrollTo(0), прокрутил вниз — не трогаем). **(3) Режим записи (FR-S8, ADR-009):** WS C→S `recording {on}` + S→C `stt_segment {text,speech_ms,total_speech_ms}` (backward-compat — только добавления). Сервер: подавление AI-хода в двух точках (runStreamTurn/handleVoiceUtterance) ПОСЛЕ фильтра доверия (шум не попадает в транскрипт) → emitRecSegment; nudge-guard (паузы внутри записи — норма); barge-in (tts_stop) работает как обычно. Клиент: stt_partial/stt_segment → окно записи (живой склеенный транскрипт + мм:сс + статус, сворачивание), НЕ в строки; «выключить микрофон»/«Отправить сейчас» → recording off + ОДНО utterance со склеенным текстом (mergeRecording: сегменты + recPartial; WS-порядок: off раньше utterance) → AI-ход; «недостаточно речи» (< MIN_SPEECH_MS = 1500 мс или пусто) — не отправлять + заметка; пауза — запись сбрасывается. Чанкование длинных записей: сегментация по тишине уже в /stt/stream (Silero, граница на тишине), клиент держит текст (KB), не аудио. **Live-проверка** (docs/test-results/voice-recording-2026-10-09.md): api с новым кодом на :18000 (отдельная БД), Silero TTS 10 фраз = 65,9 с с паузами 1,5 с (чанки тишины! — VAD считает аудио-время, не wall) → 9 stt_segment (45,25 с речи), 0 user-ходов/ai-ходов во время записи, utterance 685 зн. → ровно 1 ответ ИИ (1,8 с). Тесты: api TestRecordingModeNoAutoTurn/TestRecordingSegmentationOnSilence (детерминированные), vitest E2E (реальный хук + fake WS/mic), регресс TestVoice*/TestBargeIn* зелёные. **Бэклог:** (a) жёсткий 30-с лимит непрерывной речи (разрыв ultra-длинного speech-сегмента в /stt/stream); (b) xref: design_review-формат (T-20261009152316) — «не идеально 3–5 предложений» |
| 47 | 2026-10-09 | Voice UX — верификация и инцидент stale-бинарника (сообщено пользователем в живой сессии). **Инцидент:** api в работе — бинарник от 10-08 (до FR-S8), фронт — свежий (vite HMR): клиент шлёт `recording`/`utterance` → «unknown_ui_event: неизвестное событие: recording», PCM идёт по старому авто-пайплайну (ответ ИИ на неполную реплику при каждой паузе). Механизм «тихой» не-сборки: `go build -o <существующий файл>` при неизменённом коде НЕ переписывает файл (кэш линкера, тот же inode; проверено: mtime не меняется, exit 0). **Фиксы:** (1) `scripts/run-all.sh do_build` — сборка во временный файл + атомарный `mv` + проверка mtime (громкий сбой при не-обновлении); (2) регресс-тест `TestRecordingUIEventsNoUnknown` (api: события режима записи понимаются, нет unknown_ui_event); (3) UI по запросу: кнопка мика при включённом мике — «Отправить» (отправка буфера + выключение мика), пока ИИ отвечает — «Ожидание ИИ…» (disabled, не перебиваем), вместо «Отправить сейчас» в окне записи — «Очистить буфер» (сброс накопленного, запись продолжается — надиктовать заново; disabled при пустом). **Замеры (свежий api):** запись 65,9 с (10 фраз, паузы 1,5 с) → 9 стt-сегментов, **0 ходов/страниц кандидата во время записи**, 1 ответ ИИ после «Отправить» (685 зн., порядок сегментов сохранён); краевой случай (1924 мс речи) → 1 сегмент, 0 ходов, 1 ответ; guard «недостаточно речи» (UI, headless fake-mic) — utterance не отправляется. Скриншоты: docs/test-results/voice-ux-verify-2026-10-09/ (shot-1/2/3). Оговорка: headless fake-mic нестабилен (резервный ScriptProcessor) — живую склейку в окне проверить **на реальном микрофоне пользователя**. Регрессия: go 8/8+2/2, vitest 111/111, pytest 27/27, tsc/build OK |

- **2026-10-07** (окружение) — миграция окружения (новый хост-путь /home/arkalaust/CODE/...), восстановление запуска: (1) execute-биты слетели с scripts/*.sh и всем .venv/bin — восстановлены (chmod +x); (2) Go 1.24.5 в /home/arkalaust/sdk/go (go.mod требует 1.26 — GOTOOLCHAIN=auto докачивает 1.26); симлинк ~/.local/go-toolchain → /home/arkalaust/sdk/go (путь, который ждёт run-all.sh); (3) pnpm 12.10.1 через corepack enable --install-directory ~/.local/bin (node v22.23.3 в ~/.local/share/pi-node/current/bin); (4) **venv services/voice: симлинки .venv/bin/python{,3,3.14} превратились в Windows-LNK-заглушки (IntxLNK) при копировании с Windows-машины** — пересозданы (python3→/usr/bin/python3, python3.14/python→python3); shebang-строки 34 консольных скриптов .venv/bin/* указывали на старый путь /home/arkalaust/Code/... — sed на $PWD/.venv/bin/python. Стек 4/4 OK (api :8000 LLM qwen3.8-27b-fp8@192.168.1.114, voice :8100 cuda/large-v3, sandbox :8200, frontend :5173).

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

- **2026-09-14** (Фаза 3) — Шаг «WP-10: System Design (frontend + backend)»:
  - Backend:
    - db: WhiteboardStore (Save upsert/Get, таблица whiteboards — со WP-3,
      store был недостающим звеном); ErrWhiteboardNotFound.
    - interviewer: OnDesignSubmit — оценка схемы по рубрике ADR-004
      (покрытие/масштабируемость/отказоустойчивость/trade-offs) + follow-up,
      транскрипт устного ответа — из контекста стадии; событие ai_utterance
      (note design_review).
    - PUT /sessions/{id}/whiteboard: {state (JSON Excalidraw), png? (base64,
      MVP не обязателен), structure {blocks, links}} → upsert whiteboards +
      событие whiteboard_save + ИИ-оценка fire-and-forget (ai_text по WS).
      400 без state, 404 чужая сессия, 409 завершена; лимит тела 8 МБ.
  - Frontend:
    - api.ts: saveWhiteboard() + DesignStructure; **исправлен реальный баг**:
      request() читал поле msg, а бэкенд шлёт message (writeError) — теперь
      оба варианта, и человеческие сообщения ошибок работают.
    - DesignPanel.tsx: палитра 12 блоков (ADR-004), холст инжектится
      (prop canvas, дефолт — ExcalidrawCanvas), «Оценить схему» (заблокирована
      без блоков) → PUT /whiteboard (state из snapshot() + структура),
      ошибки — «Схема: …», блок «Оценка ИИ» (ai_text стадии design).
    - ExcalidrawCanvas.tsx: Excalidraw 0.18 (динамический import), вставка
      блока — группа rectangle+text через updateScene (addElements в 0.18
      убран), snapshot: getSceneElements/getAppState + число стрелок.
    - SessionView: стадия design → DesignPanel (designReview из ai_text,
      сброс на stage).
  - Тесты: backend +TestWhiteboardPut/Errors (save+структура+событие, 400/404;
    подвох: разные env = разные in-memory БД, но общий JWT-secret — «чужой»
    пользователь создавать внутри env); frontend +DesignPanel 6 (палитра,
    вставка, PUT body {state, structure{blocks,links}}, disabled, ошибка,
    ревью) + SessionView «стадия design» (мок @excalidraw/excalidraw через
    vi.mock — jsdom) — frontend 52, api зелёные, tsc/vite build зелёные.
  - **Нашёл баг WP-9 (исправлен)**: LiveCodePanel не синхронизировал
    taskFiles после монтирования — панель монтировалась по REST stage ещё до
    WS stage.task, и шло main.go вместо файлов задачи. useEffect по taskFiles.
    Тест SessionView livecode теперь ждёт solution.go.
  - e2e live-smoke (api LLM_MOCK): voice → livecode → design (переходы только
    по одной стадии) → PUT /whiteboard (структура 5 блоков/4 связи) →
    ИИ-оценка по WS → события whiteboard_save+ai_utterance. LIVE10 DESIGN SMOKE OK.
  - ARCHITECTURE.md v0.4.9.
  - WP-9 закоммичен (d51bc05) и запушен; roadmap: b8f824e.

- **2026-09-14** (Фаза 3) — Шаг «WP-11: Отчёт (генерация §12 + UI)»:
  - Backend:
    - db: ReportStore (Save upsert / Get, таблица reports — со WP-3).
    - interviewer/report.go: GenerateReport — LLM-оценщик (system: роль,
      шкала 1–5; user: критерии+веса §12 по грейду, транскрипт, кодовые
      запуски, схема; строго JSON) → parseReportJSON (веса из таблицы §12 —
      защита от «слитых» весов LLM, clamp 1..5, взвешенное среднее) →
      gradeRecommendation по порогам §12; fallback — heuristicReport
      (детерминированно: Live-Code 4/3/2 по запускам, Design 3/2 по схеме,
      Коммуникация 3/2 по речи, остальные 3) — mock-режим и ошибки парсинга.
    - httpapi: GET /sessions/{id}/report (200/202 generating/409 не завершена;
      JSON-массивы через json.RawMessage — []byte в map marshalится в base64!);
      startReportGeneration (go: GenerateReport → Save → WS report_ready{overall})
      — вызывается после finish в WS ('finish') и REST (handleSessionAction).
    - llm.MockProvider.SetResponder (управляемый LLM в тестах).
  - Frontend:
    - api.ts: Report/ReportCriterion, getReport() (202 → 'generating'),
      helpers authHeaders/readApiError.
    - ReportView: итог N/5, grade-рекомендация, критерии (имя, балл+вес%,
      цветная шкала 1–5: ≥4 good / ≥2.5 mid / bad), сильные стороны/зоны роста
      (колонки), рекомендации; 202 — polling (pollMs/maxPolls — props, дефолт
      2 с × 30), после лимита — «ещё генерируется» + «Повторить»; 409/сеть —
      ошибка + «Повторить».
    - SessionView: завершённая сессия → ReportView под записью; report_ready →
      refreshStatus (уже есть).
  - Тесты: backend +TestReportLifecycle (202→200, heuristic-баллы: livecode 4,
    comm 3), TestReportLLMJSON (веса §12: 4.25 → «с запасом»), TestReportErrors
    (409/404); frontend +ReportView 4 (200, 202→200 polling, 409, лимит
    опросов) + SessionView «отчёт под записью» — frontend 57, api зелёные,
    tsc/vite build зелёные.
  - e2e live-smoke (api LLM_MOCK): create → finish (REST) → GET /report
    (202 → 200: overall 2.45, 5 критериев, «ниже грейда» — пустая сессия,
    эвристика). LIVE11 REPORT SMOKE OK.
  - Подводные камни: fake timers vitest + findBy несовместимы (дедлок) —
    интервал опроса инжектится через props; user_utterance-событие пишется
    обработчиком WS, а не OnUserUtterance (она пишет ai_utterance).
  - ARCHITECTURE.md v0.4.10.
  - WP-10 закоммичен (79b58ce) и запушен; roadmap: 81cc6fc.

- **2026-09-14** (Фаза 3) — Шаг «WP-12: Инфра и доки»:
  - README.md — полный (был однострочный): документация, композиция,
    быстрый старт dev (make run-*), тесты/сборка/смоук, деплой compose,
    dev-переменные (LLM_MOCK, fake-провайдеры, SANDBOX_MODE), лицензия.
  - infra/docker-compose.yml: profile **gpu** (voice на GPU-узле:
    device-requests nvidia, STT_DEVICE/STT_MODEL из окружения); заголовки
    обновлены (черновик → WP-12).
  - Makefile: `up-gpu`, `smoke` (+ help); smoke.sh — REST e2e-контур
    (healthz → регистрация → сессия → whiteboard → report 409 до finish →
    finish → report 202 → 200 (overall/критерии) → кабинет (история,
    status finished)); без jq (python3) — `make smoke` [API=http://…].
  - .env.example — проверен, актуален (все переменные WP-1..WP-11).
  - Проверки: bash -n smoke.sh, YAML compose валиден (profiles prod/gpu,
    gpu: nvidia reservation), make help; **SMOKE OK** на живом api
    (LLM_MOCK, :8884). Docker в dev-окружении отсутствует — compose
    не запускался (валидация структуры; запуск — на nod с docker).
  - Подводной камень дня: stale-процесс api на занятом порту маскировал
    новый бинарник (405 на новых роутах) — чистить pkill перед smoke.
  - ARCHITECTURE.md v0.4.11.
  - WP-11 закоммичен (1f69796) и запушен; roadmap: 45860c6.

- **2026-09-14** (Фаза 4) — Фаза 3 (Implementation) ЗАКРЫТА: все WP-1…WP-12
  (последний — WP-12 «Инфра и доки»: README полный, compose gpu-профиль,
  scripts/smoke.sh + make smoke [SMOKE OK], Makefile up-gpu/smoke; fix флейка
  App.test — гонка /me и /sessions). Коммиты 35293da, 5621069, 86309ad,
  roadmap «Фаза 3 — ЗАВЕРШЕНА».
- **2026-09-14** (Фаза 4) — Шаг «План тестирования»: docs/TEST_PLAN.md v1.0 —
  пирамида (юнит/интеграция/E2E/load), матрица «требование → тест» по FR-A/S/C/
  S5/R и NFR; методики: голосовой latency (harness, p50/p95, эталон 20 фраз,
  STT-сходство ≥0.85), сандбокс (timeout/OOM/network=none/FS), load
  (50 WS-сессий, Go-клиент, p95 tick ≤1 с); регресс-прогон (make test ×3,
  build, smoke, liveN) и критерии выхода фазы.
  Подводные камни: в dev-окружении нет docker (compose-запуск — на nod);
  stale-процессы api на занятых портах маскируют новый бинарник (pkill перед
  e2e); jq отсутствует (smoke.sh — на python3).

- **2026-09-14** (Фаза 4) — Шаг «Тесты голосового контура» (TEST_PLAN §3):
  - Инструмент: services/voice/latency/probe.py (websockets) + эталон
    etalon20.json (20 фраз, 2–3.3 с). Методика: синтез фраз Silero TTS 24k →
    ресемплинг 16k (контракт WS, ADR-001) → WS-чанки 250 мс с непрерывным
    «микрофоном» (речь + 12 кадров тишины — VAD ждёт 900 мс тишины В ПОТОКЕ;
    останавливать отправку нельзя!); до отправки — ожидание конца TTS-приветствия
    по flag 0x0001 (turn-taking, TTS-кадры api шлёт burst'ом).
  - Подводные камни (все найдены и описаны): (1) api ждёт 16 кГц, Silero отдаёт
    24 кГц — ресемплинг 2:3; (2) greeting TTS глотает фразу (ttsActive) — ждать
    end-flag; (3) конец реплики VAD требует тишины внутри непрерывного потока —
    хвост тишины 3 с; (4) старым бинарником/портом маскируются новые роуты.
  - Замеры (CPU, 20 прогонов/модель): tiny: stt p95 6.5 с, сходство 0.68;
    small: stt p95 6.5 с, сходство 0.87 (порог 0.85 — пройден). TTS-старт ≈
    llm-старт (Silero CPU не узкое место). Декомпозиция latency: VAD-хвост 0.9 с
    + whisper int8 CPU 2.3–3.2 с + LLM (мок). SLO p95<4 с не достигается на CPU
    non-streaming — бэклог: стриминговый STT, EndSilence 700 мс, GPU large-v3.
  - Побочные находки (бэклог): LOG_LEVEL читается но не применяется к логгеру
    (debug не пишется); TTS без real-time pacing (burst) — для стриминга TTS
    потребуется pacing.
  - Отчёт: docs/test-results/voice-latency-2026-09-14.md (+json tiny/small).
  - Регресс: go vet+test (api 7 ок), tsc, vitest 57/57.

- **2026-09-14** (Фаза 4) — Шаг «Тесты сандбокса» (TEST_PLAN §4):
  - sandbox_probe_test.go (subprocess, живой раннер): вечный цикл → timeout
    (1.50 с, exit=124, [timeout]-маркер) — PASS; 2 ГБ аллокация — не зависает
    (OOM-kill — docker-сценарий); поток вывода 100 кБ → обрезка по MaxBytes
    (ровно 1024); workdir под базой, не /tmp (Go-особенность), удаление после
    запуска — PASS.
  - **Критичный баг найден и исправлен**: мёртвый цикл таймаута subprocess —
    exec.CommandContext убивает только sh, дочерние держат пайпы, Wait ждёт EOF
    вечно, Kill группы был после Wait (недостижим). Фикс: exec.Command +
    select{waitDone | ctx.Done: Kill(-pid,SIGKILL); <-waitDone} — runner.go.
  - Docker-сценарии (network=none, OOM-лимиты) — на prod-узле/CI (Фазы 5),
    бэклог: rlimit для subprocess.
  - Отчёт: docs/test-results/sandbox-2026-09-14.md.
  - Регресс: sandbox go vet+test зелёные, api 7 ок, tsc, vitest 57/57.

- **2026-09-14** (Фаза 4) — Шаг «Load-тест real-time» (TEST_PLAN §5):
  - load_test.go (httpapi, в make test): 50 параллельных WS-голосовых сессий
    × 20 с, непрерывный PCM-поток (VAD активен), LLM-мок. Результат: 50/50
    сессий живы, 200 тиков таймера (по 4, без срывов/дублей), 0 клиентских
    ошибок, 20.2 с wall. MVP-нагрузка выдержана с запасом.
  - Подводной камень: в тестах timer-сообщения не приходят, пока не запущен
    Engine().Run (в проде — cmd/api) — load-тест поднимает свой env c
    NewWithLLM + go srv.Engine().Run(ctx) + Cleanup(Stop).
  - Отчёт: раздел в docs/test-results/voice-latency-2026-09-14.md (load-секция).
  - Регресс: api go vet+test зелёные (httpapi 24 с — с load), frontend tsc +
    57/57, sandbox 2 пакета.

- **2026-09-14** (Фаза 4) — Шаг «Исправление багов, regression-прогон» (TEST_PLAN §6):
  - Баг (найден в latency-шаге): LOG_LEVEL читалось из окружения, но не
    применялось к логгеру (main.go: slog.NewJSONHandler(os.Stderr, nil)) —
    debug-логи не писались. Фикс: HandlerOptions{Level} по cfg.LogLevel
    (debug|info). Проверено: при LOG_LEVEL=debug пишутся DEBUG-строки
    (ws: pcm-статистика при обрыве WS).
  - Regression-прогон (критерий §6): go vet+test — api 7 пакетов, sandbox 2
    (×1, load-тест входит в make test), frontend tsc + vitest 57/57 ×3
    (стабильность), vite build; **SMOKE OK** на финальном бинарнике (:8903).
  - ФЛЕЙКОВ нет (3 стабильных прогона).
  - ФАЗА 4 ЗАКРЫТА (критерии TEST_PLAN §6): план, голосовой контур
    (latency+качество), сандбокс (пробы + фикс таймаут-бага), load (50 WS),
    баги исправлены, регресс зелёный. Phase gate: одобрение пользователя →
    Фаза 5 (Deployment).
  - Бэклоги фазы 4 (не блокирующие): стриминговый STT + EndSilence 700 мс
    (SLO p95<4 с), GPU large-v3 (качество 0.9+), docker-пробы в CI (Фаза 5),
    rlimit subprocess, TTS-стриминг + pacing, AEC/эхо-подавление, PNG-экспорт
    холста + vision-оценка (ADR-004), read-only task tests, Silero-VAD onnx в Go.

- **2026-09-14** (Фаза 5) — Шаг «CI/CD» (задача 1):
  - .github/workflows/ci.yml (push master + PR, concurrency): jobs
    go (api+sandbox: vet/test/build CGO=0), frontend (tsc/vitest/build),
    voice (pytest, fake-провайдеры; torch CPU ПЕРЕД requirements — иначе
    nvidia-* ~2 ГБ), smoke (build api → LLM_MOCK :8877 → scripts/smoke.sh;
    лог api при сбое). Все jobs эмулированы локально: SMOKE OK, pytest
    12 passed, go/frontend зелёные.
  - docs/DEPLOYMENT.md: prod-план (узел app 4vCPU/8GB, LLM-узел vLLM+
    Qwen3.8-27B, nginx/TLS, чеклист развёртывания) + мониторинг (SLO
    latency <4 с, STT conf ≥0.7, алерты по warn-логам) — задачи 2/3 Фазы 5
    требуют VPS пользователя (phase gate).
  - CD: артефактный (make up на узле); автодеплой по тегу — после prod-узла.
  - ARCHITECTURE.md v0.4.12.

- **2026-09-14** (Фаза 5) — Шаг «Latency-глубокий замер (уточнение)» по
  «продолжай»: доноска по голосовому контуру. Выводы (измерено):
  - whisper small int8: 2 с аудио = 0.68 с (бенч beam 5/1, vad_filter on/off —
    0.6–0.7 с, выигрыша нет); STT — НЕ узкое место на CPU для коротких реплик.
  - api→клиент WS: миллисекунды (text-utterance 3 мс; connMu/таймер-конфликт
    исключён экспериментом с реверсом); «1.8 с до transcript» в первых замерах
    — артефакт проба (WS-клиент без параллельного read-pump копится в
    TCP-буфере; реальные клиенты читают постоянно).
  - Справедливая декомпозиция «до TTS-старта»: речь + VAD-хвост 0.9 с
    (проектное, end-of-speech) + STT 0.7 с + LLM (1–3 с). Пайплайн добавляет
    ~1.6–2.2 с. SLO p95 <4 с — только стриминговый STT (chunked) или
    EndSilence 700 мс (риск обрывов) — бэклог ADR-002-обновление.
  - Отчёт voice-latency-2026-09-14.md дополнен разделом «уточнено глубинным
    замером»; engine.go — эксперимент с go sendJSON реверснут (чистый).
  - Инструменты: latency/probe.py (можно --api/--tts/--runs/--out); бенч STT
    (faster_whisper transcribe на /tmp/vadpcm.bin).

- **2026-09-14** (бэклог) — Шаг «PNG-экспорт холста + vision-оценка» (ADR-004):
  - Фронтенд: DesignCanvasApi.snapshot → async (+pngB64: Excalidraw exportToBlob,
    lazy-import, 32 px padding, null если холст пуст); DesignPanel ждёт PNG и
    шлёт в saveWhiteboard; api.ts saveWhiteboard(+pngB64?) — поле png в теле PUT.
  - API: PUT /whiteboard принимает png (base64, ≤10 МБ, 413 при превышении,
    400 при не-base64) → whiteboards.Save (png_path); оценка ИИ OnDesignSubmit(
    ..., png) → data-URL в Message.Images (llm.Message +Images []string) →
    Qwen vision в проде (OpenAI-мультимодалка), mock — детерминированно.
  - Тесты: TestWhiteboardPutPNG (PNG сохраняется + mock LLM получил
    data:image/png;base64,... в user-сообщении), TestWhiteboardPutPNGInvalidBase64
    (400); фронтенд-тесты (mock canvas async snapshot) 57/57.
  - Регресс: api 7 пакетов, tsc, vitest, vite build — зелёные.
  - Ограничение: в проде PNG идёт в LLM как data-URL (вместо native images-
    массива OpenAI) — Qwen vision принимает; при переходе на нативный формат —
    поправить llm/client.go (одна точка).
- **2026-09-14** (бэклог) — Шаг «Pre-STT (STT, перекрывающий VAD-хвост)»
  (ключевой шаг к SLO p95 <4 с на CPU без GPU):
  - Механизм: при первой тишине после речи (PreSilenceMS=400 мс, env
    VAD_PRESTT_SILENCE_MS) api запускает распознавание на текущем буфере
    реплики (vad.Detector.PreSilence/Utterance) — оно перекрывает остаток
    VAD-хвоста (EndSilenceMS 900 мс). Ходовой конвейер (handleVoiceUtterance)
    ждёт результат pre-STT (если реплика не расширялась) или запускает
    повторный STT (если речь возобновилась).
  - Инвалидация: новые speech-кадры расширяют буфер (vad.UtteranceLen растёт)
    → preSTTBytes не совпадёт → повторный STT.
  - Эффект: p95 «речь → TTS-старт» −0.5–0.7 с (на CPU без GPU: ~5.5–6.5 с →
    ~5.0–6.0 с). С реальным LLM (1–3 с) — ближе к SLO p95 <4 с.
  - Тест: TestWSVoicePreSTT (mock voice с задержкой STT 100 мс; проверка
    sttCalls == 1 — только pre-STT, повторный не нужен).
  - Regress: go vet+test (7 пакетов), tsc, vitest 57/57 — зелёные.
- **2026-09-14** (бэклог) — Шаг «Эхо-подавление (AEC упрощение MVP)»:
  - Проблема: пока клиент воспроизводит TTS-буфер (PcmPlayer), api уже слушает
    (ttsActive сбрасывается после отправки burst, а не после воспроизведения)
    → динамик → микрофон → VAD «речь кандидата» (эхо).
  - Фикс (упрощение, без Web-Audio AEC): SessionView onChunk — если
    playerRef.current?.isSpeaking() — чанк не шлём (микрофон «заглушаем»
    на время воспроизведения).
  - Полное AEC (Web Audio API, echoCancellation) — бэклог (сложнее: требует
    отдельного аудио-контекста, обработки в реальном времени, калибровки
    задержек).
  - Regress: go vet+test (7 пакетов), tsc, vitest 57/57, vite build — зелёные.
- **2026-09-14** (бэклог) — Шаг «TTS-стриминг по предложениям» (первый звук
  через ~0.3 с после LLM, а не после синтеза всего ответа):
  - Механизм: streamAIAudio — текст реплики разбивается на предложения
    (splitSentences: «!»/«?» всегда, «.» только перед заглавной — русские
    аббревиатуры «т.д.» с малой не дробят, многоточие «...» — разрыв), каждое
    синтезируется отдельно и отправляется сразу (seq сквозной по реплике,
    end-флаг — только на последнем кадре последнего предложения).
  - Дegrade: ошибка синтеза одного предложения — log warn + skip (голос не
    критичен, текст уже есть).
  - Тесты: TestSplitSentences (5 кейсов), TestTTSStreaming (2 предложения →
    2 вызова TTS). Regress: go vet+test (7 пакетов) — зелёные.
  - Эффект: perceived latency «LLM → первый звук» −(дл. синтеза всего ответа −
    дл. синтеза первого предложения) ≈ −0.5–2 с (зависит от длины ответа).
- **2026-09-15** (фича) — ограничение времени сессии снято в dev-режиме:
  - env SESSION_LIMIT_S (api): пусто/не задан 0-значный дефолт = по грейду
    (45/50/60/75 мин); 0/off/none -1 (без ограничения: финализация по
    времени и WS timer отключены, duration_limit_s=0); N - фикс. лимит, с.
  - session.Engine: WithSessionLimit(s) Opt; tick финализирует/шлёт timer
    только при limitS>0; Create применяет оверхейд.
  - run-all.sh: SESSION_LIMIT_S=off по умолчанию (dev), переопределяется;
    README «Быстрый старт» - вариант.
  - Тесты: TestSessionLimitUnlimited (9999 с - остаётся active),
    TestSessionLimitFixed (600 с - финиш). Регрессия зелёная (7 пакетов).
- **2026-09-16** (багфикс) — AudioWorklet в браузере пользователя молчит
  (скриншот: «Микрофон включён, но аудио-данные не приходят» — мой новый
  no-data-диагност триггернулся; worklet addModule ок, process() не
  вызывается — воспроизведено в headless: без аудио-устройства аудио-
  поток браузера не гоняется, даже осциллятор -> worklet). Ответ:
  (a) авто-фолбэк в mic.ts: 3 с без чанков -> legacy ScriptProcessorNode
      (main thread, resampleToPcm16), worklet отключается (двойного
      потока нет); если и SP молчит 3 с — предупреждение в UI;
  (b) audio-debug.html: кнопка «Тест AudioWorklet» — тот же процессор
      (копия PcmCapture + счётчик process), 4 с: process-вызовы / чанки /
      level / maxAbs / ctx.state — определяет, жив ли аудио-поток в
      браузере пользователя.
- **2026-09-16** (багфикс + фича) — голосовой контур, 4 замечания:
  1. Эквалайзер «не прыгал»: компонент MicVisualizer никогда не рендерился —
     python-вставка в JSX была no-op (индент не совпал, без assert).
     Исправлено: элемент в .voice-controls. Плюс диагностика в mic.ts:
     addModule/контекст — ошибки с текстом в UI; 3 с без первого чанка —
     предупреждение «данные не приходят».
  2. Фразы ИИ сливались: в streamAIAudio между предложениями — тишина
     250 мс (после .), 500 мс (после ?/!); финальный end-кадр 250 мс
     тишины со стоп-флагом (только если PCM реально шёл — иначе WS-тесты).
  3. Латиница не озвучивалась: tts_text.go prepareTTS — карта терминов
     (Go=гоу, C++=си плюс, 60+ терминов) + фолбэк транслит по буквам;
     применяется ТОЛЬКО к потоку TTS, текст в UI оригинальный.
     Regex латинского слова БЕЗ дефиса («Go-сервис» -> Go отдельно).
  4. Женский голос / мужской текст: TTS_SPEAKER=aidar в run-all.sh
     (дефолт voice), персона в промпте — мужчина, говори в м.р.;
     промпт: интонация по знакам (? ! ,), без сокращений т.д./т.п..
  Замер громкости: aidar rms~2000, ru_01 ~2260, eugene ~1910.
  Лимит времени сессии снят (SESSION_LIMIT_S=off в run-all, см. выше).
  Локальная headless-проверка worklet невозможна (нет аудио-устройства
  в контейнере: аудио-поток браузера не гоняется, даже осциллятор)
  — диагностика теперь в UI у пользователя.
- **2026-09-15** (багфикс) — «ИИ не слышит кандидата» (второй уровень):
  - Диагностика: mic-test (страница audio-debug.html) показал здоровый уровень
    (RMS 2280, peak 23637) — микрофон и браузер в порядке; зонд C->S через
    vite-прокси — сервер принимает бинарные кадры (pcm-debug: rms 5656). Но
    PCM от кандидата в сессиях = 0.
  - Причина: эхо-подавление MVP — в onChunk `if (player.isSpeaking()) return;`
    + флаг speaking мог залипать (часы suspended AudioContext стоят, проверка
    очереди не выполняется). ИИ при этом говорит почти непрерывно (вопрос ->
    8 с тишины -> nudge -> вопрос), окно для ответа кандидата не открывается.
  - Фиксы: (1) player.ts watchdog — флаг сбрасывается, если >3 с нет новых
    кадров и очередь пуста (даже при остановленных часах контекста);
    (2) player.resume() + вызов из клика «Включить микрофон» (user-gesture —
    легальный resume контекста по политике Chrome);
    (3) UI-подсказка «ИИ говорит — чтобы ответить, дождитесь паузы».
  - ws.go: временный pcm-debug (Info, первый + каждый 40-й кадр, rms) для
    дальнейшей диагностики; опустить до Debug после стабилизации.
  - audio-debug.html: секция «Проверить микрофон» (уровень + 5 с записи в WAV);
    заметный баг записи: analyser fftSize=1024 отдаёт 21 мс аудио за такт, а не
    100 мс — файл короче (1.07 с вместо 5 с); для уровня не критично.
  - Проверка: tsc, vitest 59/59, vite build — зелёные. Ждём повторного теста
    кандидата (ожидание: pcm-debug с уровнем + POST /api/v1/stt).
- **2026-09-15** (багфикс) — звук ИИ обрывается после первых слов (причина найдена):
  - Корень: PcmPlayer.playChunk — pump() вызывался только при первом кадре
    (startPlayback). Kадры TTS приходят потоком burst-ами по фразам: первый
    burst (0.7 c) проигрывался, последующие копились в очереди и не
    закачивались (playing=true, startPlayback не идёт, очередь не пустеет
    без pump). Симптом: первая половина слова и тишина.
  - Фикс: playChunk после каждого push делает pump(this.ctx), если
    воспроизведение уже идёт. Регресс-тест src/audio/player.test.ts (мок
    AudioContext, burst-кадры): без фикса падает (проверено git stash).
  - Инструмент: services/frontend/public/audio-debug.html — запись каждой
    реплики в WAV (кнопка скачивания) для диагностики.
  - Проверка: tsc, vitest 59/59 (12 файлов), vite build — зелёные.
- **2026-09-15** (инфра) — «как запускать после перезагрузки»:
  - scripts/run-all.sh (start|stop|status|restart): сборка (FOR_RUN/bin) +
    sandbox/voice/api/frontend через nohup; PID — FOR_RUN/pids/, логи —
    FOR_RUN/logs/, БД — FOR_RUN/run.db (переживает перезагрузку). Stop — по
    pid-файлу + подстраховка по порту (ss). Готовность: опрос health до 60 с.
  - Makefile: make run-all / stop-all / status (+ help-строки).
  - README «Быстрый старт» шаг 3 — make run-all (вручную — в details-блоке);
    варианты: LLM_MOCK=1 (без узла), LLM_BASE_URL/LLM_MODEL (свой узел),
    STT_MODEL.
  - smoke.sh: SMOKE_REPORT_WAIT_S (дефолт 120 с; цикл по времени, не
    итерациям — был баг: «120» = 120 итераций × 0.2 с ≈ 30 с).
  - session_handlers: убран двойной WriteHeader 202 в GET /report
    (90+ предупреждений «superfluous response.WriteHeader» в логе);
    server.go statusRecorder: статус фиксируется первым WriteHeader.
  - llm.Client: slog.Debug-логи запроса/ответа (диагностика LLM-узла).
  - Проверка: make status — 4 сервиса OK; SMOKE OK с реальным LLM (отчёт
    ~30-60 с, reasoning-модель). Регресс: зелёные.
  - Ограничение: сервисы, запущенные из среды ИИ-агента, не переживают
    завершение сессии агента (kill process tree) — для постоянной работы
    `make run-all` запускать в собственном терминале (или tmux/systemd).
- **2026-09-15** (багфикс) — «Голос ИИ обрывается на ПРиве» + доработка nudge:
  - **Плеер TTS: AudioContext(16000) → частота устройства** (player.ts):
    экзотический 16-кГц контекст нестабильно воспроизводится на некоторых
    аудио-стеках (PipeWire/PulseAudio) — звук «обрывался» после первого слова
    (проверено: api шлёт все кадры + end-флаг, TTS-синтез полный и громкий —
    дело в воспроизведении). Теперь AudioContext по умолчанию (44.1/48 кГц),
    PCM 16 кГц ресемплируется линейно в playChunk (resampleFloat, resample.ts).
  - **Nudge-поток затапливал UI/LLM-контекст** («быстро сменяющийся текст»):
    nudge-цикл обновлял lastActive (ws.touch в streamAIAudio + touch в nudgeLoop)
    → отсчёт тишины не обнулялся корректно → бесконечные nudge каждые 2-4 с.
    Фикс: (1) отсчёт тишины — от активности КАНДИДАТА (lastPCM из PCM-кадров,
    lastCandActivity), nudge её не обновляет; (2) лимиты: cooldown 15 с,
    max 3 подряд (сброс при активности); (3) nudge-реплики теперь озвучиваются
    (FR-S3: пауза заполняется голосом, streamAIAudio в nudgeLoop); (4) UI:
    подсказка «Микрофон выключен — ИИ вас не слышит» (stage voice, mic off).
  - Проверено зондом (без микрофона): приветствие + ровно 3 nudge (10/28/45 с),
    дальше тишина (лимит сработал). В браузере с включённым микрофоном nudge
    не fires (PCM-кадры обновляют lastPCM).
  - Regress: зелёные (go 7 пакетов, tsc, vitest 57/57, vite build).
- **2026-09-15** (багфикс) — «ИИ не слышит + быстро сменяющийся текст» (тест в
  браузере). Диагноз живым PCM-зондом (реальное время, WS):
  - Конвейер (VAD → pre-STT → LLM → TTS) работает, НО: (1) фиксированный
    VAD-порог RMS 500 не слышал тихий микрофон (PCM ~RMS 100–300) — «ИИ не
    слышит»; (2) шум/дыхание (всплески ≥ 500) проходили как «реплики» → STT
    галлюцинации → «быстро сменяющийся текст».
  - Фикс 1 (vad): АДАПТИВНЫЙ ПОРОГ — шумовой пол (EMA: мгновенно вниз,
    α=0.05 вверх; заводка после 20 тихих кадров ≈ 5 с) + gain 3:
    threshold = max(RMSThreshold=100, 3×noiseFloor). Тихая речь детектируется
    (порог следует за фоном), шум/всплески фона — нет.
  - Фикс 2 (voice_pipeline): ФИЛЬТР ДОВЕРИЯ — res.Confidence < 0.5 → шум,
    без хода (реальные реплики conf ≥ 0.7 по замерам; галлюцинации на шуме — ниже).
  - VAD_RMS_THRESHOLD: 500 → 100 (абсолютный минимум; фактический — адаптивный).
  - Тесты: TestVADAdaptiveNoiseFloor (фон 5 с → тихая речь RMS ~282 детектится,
    хотя фиксированный 500 нет), TestVADNoiseBurstNotUtterance (всплеск 250 мс
    ≠ реплика). Regress: go 7 пакетов, tsc, vitest 57/57.
  - Live: тихая речь (TTS ×8) через WS → pre-STT 98 chars conf 0.693 → ход ИИ.
  - Остаток (бэклог): AGC/AEC на фронтенде (Web-Audio normaliser) — точная
    калибровка под конкретный микрофон кандидата.
- **2026-09-15** (багфикс) — Два бага из пользовательского теста:
  1. **Голос в Live-Code/Design**: вход на стадии не озвучивался (streamAIAudio
     вызывался только для voice-приветствия). Фикс: enterLiveCode (+ws) и
     design-вход (stage_action) — после sendInterviewerText — streamAIAudio(ws, text)
     (озвучка задачи/формата стадии). Проверено: 146 TTS-кадров (~11 с аудио)
     при переходе в livecode.
  2. **Пустой контент LLM (reasoning-модели)**: qwen3.8-27b-dflash2 —
     reasoning-модель; при finish_reason=length весь бюджет max_tokens (300)
     уходил на рассуждение (reasoning ~1000 токенов), content=None → пустой
     ai_text + нет TTS. Фикс: llm.Client.Chat — при finish_reason=length и
     пустом content — повтор с увеличенным max_tokens (×2, min 1500). DefaultTimeout
     30→60 с (reasoning-модели медленнее).
  3. **Программа по грейдам в voice-стадии**: добавлено gradeProgram (блоки
     программы из прототипа лендинга, SRS §4.2) в system-промпт voice-стадии:
     Junior 4–6 вопросов (3 блока), Middle 6–8 (4 блока), Senior/Staff 8–10
     (4 блока). ИИ ведёт диалог по блокам (1–2 вопроса на блок).
  - Regress: go vet+test (7 пакетов), tsc, vitest 57/57 — зелёные.
  - API перезапущен с новым бинарником (grade-api-run4, LOG_LEVEL=debug).
- **2026-09-14** (доки) — README: «Быстрый старт (dev)» — 5 шагов (установка,
  модели в FOR_RUN/, запуск 4 сервисов, подключение LLM-узла, браузер),
  таблица dev-переменных, остановка. Коммит (см. ниже).
- **2026-09-14** (запуск) — «Вся система работает на реальном LLM»:
  - Модели: FOR_RUN/ (в .gitignore): stt/Systran/faster-whisper-small (~460 МБ),
    tts/silero-tts-v5_ru.pt (~150 МБ). Скачаны scripts/download-models.sh
    (MODELS_DIR=$PWD/FOR_RUN, LLM пропущен).
  - LLM: qwen3.8-27b-dflash2 на http://192.168.1.114:8000/v1 (vLLM, max_model_len
    262K) — /v1/models проверен; прямой chat/completions работает (2.9 с,
    reasoning-модель: поле reasoning в ответе, content заполнен).
  - Стек (запущен): sandbox :8200 (subprocess), voice :8100 (STT_MODEL=small,
    STT_DOWNLOAD_ROOT=FOR_RUN/stt, TTS_MODEL_DIR=FOR_RUN/tts), api :8000
    (LLM_MOCK=0, LLM_BASE_URL=http://192.168.1.114:8000/v1, LLM_MODEL=
    qwen3.8-27b-dflash2), frontend :5173 (vite dev).
  - Проверено end-to-end: (1) отчёт реального LLM готов (overall 2.45/2.6,
    ~25 с — LLM медленнее мок); (2) голосовой ход: приветствие Qwen3.8 →
    TTS Silero → реплика кандидата (текст + PCM: STT 2.6 с, conf 0.787) →
    ответ LLM («Какие метрики говорят, что запрос стал узким местом…») + TTS;
    (3) REST smoke-контур (report 202→200; ожидание smoke.sh 6 с < реального
    LLM ~25 с — для smoke использовать LLM_MOCK=1 или увеличить wait в smoke.sh).
  - PIDs: /tmp/{sbxrun,voicerun,apirun,front}.pid; логи /tmp/{sbxrun,voicerun,
    apirun,frontlog}.log; БД /tmp/run.db.
  - Замечание: LLM-ход ~6 с (reasoning-модель без стриминга) — стриминг LLM
    (SSE) — следующий шаг к SLO p95<4 с (после pre-STT и TTS-стриминга).
- **2026-09-14** (бэклог) — Шаг «Доверенность тестов задач» (TEST_PLAN §4.4,
  security-баг): кандидат сдавал /runs со своим набором файлов ВКЛЮЧАЯ тесты —
  тест-«троян» (всегда pass) проходил вместо тестов банка. Фикс в sandbox
  (cmd/sandbox/main.go): при task_id тестовые файлы (go: *_test.go, python:
  test_*.py/*_test.py) подставляются из банка поверх файлов кандидата;
  кандидатские файлы-решения не тронуты. Тест: TestRunTestFileTrust
  (голый solution.go + троян-тест → passed=false, сборка падает на
  «undefined: FizzBuzz» из тестов БАНКА — подстановка подтверждена).
  Регресс: sandbox зелёные (2 пакета).

2. Бэклог-улучшения (после phase gate по prod-узлу): стриминговый STT
   (chunked faster-whisper), TTS-стриминг + pacing, AEC/эхо-подавление,
   Silero-VAD onnx в Go, read-only task tests, GPU-конфиг (large-v3).
3. Phase gate: prod-узел (VPS) от пользователя → задачи 2/3 Фазы 5.
- **2026-09-28** (latency SLO) — Стриминг LLM (SSE) + до-стриминг TTS + pacing + /metrics, коммит 3cc09d8:
  - llm: `ChatStream` (SSE stream:true, парсинг data:-чанков; reasoning-модель —
    delta.reasoning отбрасывается, в ответ не входит); length-повтор (×max_tokens,
    min 1500) сохранён, дублирование кода в Chat убрано (doOnce); MockProvider —
    через Chat. Тесты: SSE-парсинг, [DONE], length-повтор, ошибка соединения.
  - interviewer: `OnUserUtteranceStream` (чан фраз, тот же промпт/контекст);
    ai_utterance в БД — ПО ЗАВЕРШЕНИИ стрима.
  - voice_pipeline: runCandidateTurn — LLM-токены → splitDeltas (предложения) →
    TTS-воркер параллельно генерации → paceFrames (≤1 кадр/250 мс, end-флаг,
    ttsActive=false строго после end-кадра). ai_text/transcript — единым сообщением
    по завершении LLM-текста (frontend использует ai_text как финальный).
    streamAIAudio (приветствие/nudge/входы на стадию) — без изменений, burst-долг там.
  - metrics (новый пакет, без зависимостей): grade_ai_turn_seconds{stage=
    llm_first_token|tts_first_frame|turn_end} + счётчики ошибок; GET /metrics.
  - LLM_MODEL: qwen3.8-27b-dflash2 → qwen3.8-27b-fp8 (на узле 192.168.1.114:8000
    старое имя — 404, проверено live). SSE-поддержка узла подтверждена зондом.
  - Live-замер (fp8, fake STT/TTS): первый TTS-кадр 7.5–12 с (ход), накладка
    конвейера (первый content-токен → первый кадр) 3 мс–0.3 с; сырой замер к
    vLLM: reasoning-токены 2.7–7.9 с до первого content-токена — узкое место
    теперь LLM-узел, SLO p95<4 с упирается в него, а не в конвейер.
  - Регресс: go vet+test (7 пакетов) зелёные, vitest 59/59, frontend не тронут.
  - Ограничение (как было и до): finish_reason=length с НЕПУСТЫМ контентом
    (обрезка по max_tokens=300) — повтор только при пустом контенте.
  - Следующее (по решению пользователя): ограничение reasoning-бюджета на
    LLM-узле / более быстрый сервинг — инфраструктурное.
- **2026-09-28** (Фаза 5, артефакты; НЕ закоммичено — ждёт ревью) — Prod-состав
  с TLS + мониторинг:
  - compose (infra/docker-compose.yml): caddy (prod; GRADE_DOMAIN, CADDY_TLS=
    letsencrypt|internal; всё → frontend:80, nginx уже проксирует /api/, /healthz,
    /ws с Upgrade — отдельного маршрутизации в caddy не нужно; frontend-prod
    НЕ нужен: frontend.Dockerfile уже multi-stage nginx+dist).
  - monitoring-профиль: prometheus v2.53 (retention 7d, скрейп api:8000/metrics +
    self) + grafana 11.1 (provisioned datasource + дашборд infra/monitoring/
    grafana-dashboard.json: SLO-стат p95 llm_first_token < 4 с, p50/p95 по
    стадиям grade_ai_turn_seconds, rate ходов/мин, ошибки LLM/TTS).
  - prod-гигиена: restart: unless-stopped на всех prod-сервисах; voice — limits
    (VOICE_CPU_LIMIT/VOICE_MEM_LIMIT) + mount ../models:/models:ro
    (STT_DOWNLOAD_ROOT=/models/stt, TTS_MODEL_DIR=/models/tts).
  - Dockerfile: HEALTHCHECK добавлен во все 4 (api/sandbox/frontend — wget;
    voice — python urllib, curl в slim нет).
  - .env.example: GRADE_DOMAIN, CADDY_TLS, VOICE_CPU_LIMIT, VOICE_MEM_LIMIT,
    GRAFANA_ADMIN_USER/PASSWORD. docs/OPERATIONS.md (новый runbook) + ссылка в README.
  - Отклонения от постановки: БД — Postgres (volume pgdata уже был), run.db/sqlite
    в prod не используется (бэкап — pg_dump); фактический WS-путь /ws/session/{id}
    (не /api/v1/sessions/{id}/ws); профили в compose — prod/gpu (не default/gpu).
  - Проверка: docker в окружении агента отсутствует → YAML/JSON валидированы
    python (compose config не запускался), контейнеры не поднимались, коммита нет.
- **2026-09-28** (latency SLO) — Отключение reasoning-фазы LLM, коммит 847f319:
  - Узел vLLM принимает в теле /chat/completions `chat_template_kwargs:
    {"enable_thinking": false}` (проверено зондом: content-токены сразу).
  - config: LLM_ENABLE_THINKING (дефолт false); llm.Client.WithEnableThinking
    (wire-поле в doRequest — общий для Chat/ChatStream); тест на тело запроса.
  - Live-замер (fp8, fake TTS): grade_ai_turn_seconds llm_first_token avg
    7.38с→0.70с (все <1с), tts_first_frame 7.49→0.78с, turn_end 8.80→2.60с.
    SLO p95<4с по первому звуку — достигается.
- **2026-09-28** (рефакторинг долга) — коммиты после 9f18dfb:
  - api: session/engine.go 651 → engine.go(230)+engine_lifecycle.go(309)+
    engine_timer.go(77)+engine_events.go(62) — чистый перенос методов, API пакета
    не изменён. httpapi: вынесены дубли speak() (sendInterviewerText+условный
    streamAIAudio, 3 вхождения), ownedSession() (GetOwned+404/500, 3 хендлера),
    parseSessionID в WS; чистый выигрыш ~10 строк (пересечение оказалось слабым).
  - frontend: SessionView.tsx 403→197 + hooks/useVoiceSession.ts (258): WS-цикл,
    PCM-кадры/плеер, микрофон/эхо, таймер, маршрутизация ai_text — хук; JSX не
    менялся. Тесты SessionView.test.tsx без правок.
  - Регресс: go vet+test -count=1 (все пакеты), tsc, vitest 59/59, vite build —
    зелёные. gofmt поправлен в voice_pipeline{,_test}.go (был неровным с 3cc09d8).
- **2026-09-28** (инфра dev) — «make run-all: go: command not found» (коммит после dc0dce0):
  - Причина: Go-тулчейн и voice-venv жили во временных каталогах рабочей сессии
    (/tmp/go, временный venv) — не переживают перезагрузку/очистку /tmp.
  - Фикс: тулчейн перенесён в $HOME/.local/go-toolchain (путь, который уже ждёт
    run-all.sh); run-all.sh — фолбэк /tmp/go + явная ошибка вместо Error 127.
  - services/voice/.venv пересоздан (python3 venv + torch CPU + requirements.txt) —
    voice :8100 снова поднимается; стек 4/4 OK после make run-all.
  - Известное: сервисы, запущенные из-под ИИ-агента, убиваются вместе с деревом
    процессов агента — постоянный запуск: make run-all в собственном терминале
    (или tmux). make install всё ещё требует pnpm в PATH (node_modules есть).
- **2026-10-05** (pipeline T-20261005080715 R1) — Локальный voice-контур (задача 0 из очереди p-grade):
  - Модели скачаны в `services/voice/models/` (gitignore): STT — faster-whisper
    large-v3 (2.9 ГБ, HF Systran, `STT_DOWNLOAD_ROOT=…/models/stt`), TTS —
    Silero v5_ru.pt (145 МБ, models.silero.ai, `TTS_MODEL_DIR=…/models/tts`).
  - run-all.sh: модели по умолчанию в `services/voice/models/`, `STT_MODEL=large-v3`,
    `STT_DEVICE=cuda`, `STT_COMPUTE_TYPE=float16`; `.env.example` обновлён.
  - **Подводный камень №1**: ctranslate2 4.8.2 из PyPI НЕ бандлит CUDA — на GPU
    «RuntimeError: Library libcublas.so.12 is not found». Фикс: `pip install
    nvidia-cublas-cu12 nvidia-cudnn-cu12` в voice-venv (~1.2 ГБ) + LD_LIBRARY_PATH
    из `site-packages/nvidia/{cublas,cuda_nvrtc,cudnn}/lib` — run-all.sh собирает
    автоматически (поиск по glob `lib/python3.*`). CPU-ноды: не ставить, STT_DEVICE=cpu.
  - **Подводный камень №2**: `pkill -f "uvicorn app.main:app"` убивает и свою же
    bash-команду (паттерн в строке); stale-uvicorn на :8100 без LD_LIBRARY_PATH
    маскирует новый процесс (порт занят → новый умирает, старый отвечает cublas-ошибками).
  - **Подводный камень №3**: `run-all.sh start` из-под bash-инструмента агента с
    таймаутом — таймаут убивает всю процесс-группу включая nohup-сервисы. Надёжный
    запуск: `setsid bash scripts/run-all.sh start &` (своя сессия).
  - Пробы (реальные): LLM-узел chat/completions 1.0 с (enable_thinking=false);
    TTS 4.9 с аудио за 1.6 с; STT GPU large-v3 — 5 с аудио за 0.2 с (CPU было
    2.5–3 с), тишина→""; roundtrip TTS→STT conf 0.857; latency-проба (3 фразы,
    live-стек): сходство 0.846, первый TTS-кадр +0.03 с после транскрипта
    (LLM TTFB <1 с); LLM-отчёт (finish→report 200) 18 с.
  - Регресс: pytest 12 passed (fake), go vet+test (api 7 пакетов, sandbox 2),
    tsc, vitest 59/59, vite build — зелёные. Стек 4/4 OK (run-all, setsid).
  - Коммит: [см. git log] + roadmap (Фаза 6: локальный voice-контур).
  - **2026-10-05** — full-duplex barge-in (pipeline T-20261005093230 R1): см. решение #33.
    Live-проба (FOR_RUN/probe-bargein.py, реальные voice/LLM): во время pacing-стрима хода ИИ
    синтетическая реплика (тон 500 мс + тишина 1250 мс) → tts_stop мгновенно, после него
    ровно один end-кадр, grade_barge_ins_total=1, log Info «barge-in: кандидат прервал речь ИИ».
    Go-тесты: TestBargeIn_InterruptsTTS, TestBargeIn_ShortUtteranceDoesNotInterrupt (PASS).
    Регресс: pytest 12, go vet+test (api, sandbox), tsc, vitest 61/61, vite build — зелёные.
    Коммит a3bfc11 + roadmap.
  - **2026-10-05** — диагностика «молчащего» микрофона (pipeline T-20261005121104 R1): см. решение #34.
    Live-проверка (Playwright headless, FOR_RUN/live-mic-check.js): A) мьютный вход (track.enabled=false —
    честные нули) → «Микрофон молчит» в UI за 5–8 с, состояние muted; C) «рабочий» вход —
    реальный TTS-аудиофайл, поданный как audio-трек: ложных «молчит»/fallback-ошибок нет,
    транскрипт в UI, log `stt: реплика кандидата` chars=50/131 conf=0.845/0.93, живой диалог с ИИ.
    Нюанс: в headless AudioWorklet не расписывается → fallback-инфо видно в live-сценариях;
    на реальном устройстве worklet-путь работает (сессии 11/12: кадры 8000 Б). Скриншоты —
    screenshots/mic-silence/ (каталог gitignored — в git не попадают).
    Регресс: tsc, vitest 68/68, vite build — зелёные. Коммит 881eaae + docs + roadmap.
  - **2026-10-05** — стриминговый STT + Silero VAD + pre-STT (pipeline T-20261005132522 R1): см. решение #35.
    Voice: WS /api/v1/stt/stream (Silero VAD onnx, partial ≤1/500 мс, final по тишине 600 мс;
    health: `"vad":"silero"`). API: partial → WS stt_partial, final → ход кандидата; barge-in
    теперь и во время речи (500 мс от state=true); fallback на batch при недоступности стрима
    (метрика grade_stt_stream_fallbacks_total). Live-замеры (реальный стек, 3-с реплика, ту же
    пробу на старом коде из git worktree): транскрипт «до» 754 мс → «после» 695 мс (694/693/695);
    первый partial за 2.1 с до конца речи (раньше — ничего до финала); ложных final на тишине
    нет (2 пробы × 10 с); barge-in: tts_stop +500 мс от начала речи, grade_barge_ins_total=1.
    UI: интеримная строка кандидата (курсив + «распознаётся ⋯»), final фиксирует; скриншоты
    screenshots/stt-streaming/. Найден баг старых latency-проб (CHUNK 4000 Б = 125 мс вместо
    250 мс): зафиксированный ранее «baseline 2004 мс» — на 1.5-с реплике; новый A/B — на одной
    и той же 3-с реплике. Регресс: pytest 17, go vet+test (api, sandbox), tsc, vitest 69/69,
    vite build, gofmt чистый — зелёные. Коммиты ab0bc3f, 6556b72, e00b4c6, abb57f6 + roadmap.
  - **2026-10-06** — параллельный TTS-синтез (pipeline T-20261005184303 R1): см. решение #36.
    produceOrderedTTS в streamCandidateTurn (пул TTSParallelism=3, реордер по idx, порядок
    кадров сохранён) + метрика grade_ai_tts_synth_seconds. Тест TestTTSParallelSynthesis
    (mock TTS: задержка 700 мс ≥ pacing-окна + амплитудные маркеры): s2-s1=388 мкс << delay
    (перекрытие), кадры A1 полностью предшествуют A2 (порядок). Live A/B (тот же 3-с промпт,
    старый/новый бинарник): first_tts 388/435/730→367/359/433 мс (в пределах LLM-шума);
    межфразовые разрывы ~без изменений (750–1500 мс) — узкое место LLM-стриминг, синтез
    ~89 мс/предложение (21 предл., метрика) — TTS не bottleneck; механизм работает (тест
    доказал перекрытие), live-выигрыш проявляется при медленном синтезе. Инсайт: nhooyr
    conn.Read(ctx) закрывает соединение при истечении ctx → read-таймаут теста 2 с > паузы
    синтеза. Регресс: pytest 17, go vet+test+race (httpapi), tsc, vitest 69/69, vite build,
    gofmt — зелёные. Коммиты f2aa87e, 94d45e9 + roadmap.
  - **2026-10-08** — пауза/возобновление сессии, полный контур (pipeline T-20261008154023 R1): см. решение #37.
    Три подагента (backend/frontend/docs, ветки agent/*/1, merge в master):
    c3a96c1/0062eab (api), cfc27ed/06fd5e5 (frontend), da96da0 (docs),
    merge fed3c4a/26419c4/5e2a31c. Закрыт дефект: pause не останавливал активный TTS-стрим
    и не блокировал новые ходы атомарно (гонка статус БД vs движок). Регресс после
    интеграции: go vet+test (api, sandbox), pytest 17, tsc, vitest 73/73, vite build —
    зелёные; секреты/служебные файлы в merge не попали. Live-снимки UI: live-стек
    LLM_MOCK=1 + playwright (до/после/пауза). Микроскопическая гонка beginTTS-окна —
    в бэклоге (session-level stop-канал) — **закрыта в раунде 2** (см. ниже).
  - **2026-10-08** — раунд 2 (pipeline T-20261008154023 R2): session-level стоп TTS-канала
    (требования planner'а R1–R6) + замечание судьи I1. Backend (d786ac3/a93356a):
    `sessStopped atomic.Bool` на wsSession — ставится pause и finish
    (stopTTSOnAction), сбрасывается resume; `beginTTS` при флаге возвращает закрытый
    канал (гонка «ход прошёл статус-гард, но не дошёл до beginTTS» закрыта), флаг
    проверяется во всех циклах шлюзания кадров (paceFrames/streamAIAudio/pushTTSJob/
    диспетчер produceOrderedTTS); finish во время стрима — end-кадр + tts_stop.
    Семантика: pause/finish шлют tts_stop ВСЕГДА (даже без активного стрима).
    Тесты: TestSessionPauseDuringLLMPhase (пауза в LLM-фазе → 0 TTS-кадров),
    TestSessionFinishDuringTTSStream (finish во время стрима → tts_stop + end-кадр +
    тишина после). llm/mock.go: SetStreamReqDelay для детерминизма. Frontend (7c1828e,
    судья I1): unit-тесты debugInfo()/feedSilence (7) и рендера span.mic-dbg (2) —
    vitest 82. Docs (9f43f6b/a48645b): поправка ADR-007 + README «Голос (TTS)».
    Merge в master: 02278c7/09f3b48/b43253a. Регресс после интеграции (fresh
    httpapi -count=1): go vet+test (api, sandbox), gofmt пуст, pytest 17, tsc,
    vitest 82/82, vite build — зелёные. UI-изменений в раунде нет — скриншоты не
    требовались (R6). Push — по-прежнему не выполнялся (решение пользователя).
  - **2026-10-08** — надёжность захвата микрофона (T-20261008175905 R1): см. решение #38.
    Выбрана приоритетная задача 2 spec (обоснование выбора — там же; №1/№3 — в бэклоге).
    Два подагента (frontend/docs, ветки agent/*/1): 66f8b2b/6eaa52f (frontend,
    rebase на пользовательский master 40bf76c — git-протокол), 97e1a16/fac819e/e6dd268
    (docs: ADR-008 + индекс, TEST_PLAN §2.7, OPERATIONS troubleshooting).
    Merge c0d2d8a/6feab07 + доки. Факт: во время раунда пользователь параллельно
    закоммитил 40bf76c (dev-диагностика за флагами ENABLE_DEBUG/VITE_MIC_DEBUG,
    фикс пути БД sqlite) — frontend-агент перебазил ветку и перепрогнал тесты.
    Регресс: go vet+test fresh (api 47.6 с, sandbox), gofmt, pytest 17, tsc,
    vitest 91/91, vite build — зелёные; проба STT-стрима до/после без регрессии
    (docs/test-results/mic-capture-reliability-2026-10-08.md). Push — не выполнялся.
  - **2026-10-08** — клиуза-диспетчизация TTS (T-20261008185701, приоритетная задача 3): см. решение #39.
    Выбор задачи: №2 (микрофон) уже выполнена в T-20261008175905 (решение #38); №1
    (Go-VAD onnx) остаётся в бэклоге (onnx-зависимость в Go тяжёлая, эффект ограниченный —
    основной стриминговый путь уже Silero VAD на voice-сервисе, Go-VAD — только batch-fallback);
    №3 — прямое попадание в SLO p95 «речь → первый звук».
    Два подагента (backend: 0124928/fcc9994/56e2e4a; docs: 52cdb43/c48f2b2 — поправка ADR-002
    + TEST_PLAN), merge 1378046/8ec6858. По пути найдены и закрыты 2 дефекта: склейка слов
    в splitDeltas (обрезка хвоста pending) и потеря хвоста закрытого предложения.
    A/B на живом LLM-узле (3 рана/бинарник): без регрессии (first_tts 1699→1638 мс,
    разрывы 19500→19250 мс; шум LLM маскирует эффект — механизм доказан
    детерминированным тестом: клиуз уходит в TTS за 1.0 с до конца LLM-стрима).
    Регресс: go vet+test fresh (api 54.3 с, sandbox), gofmt, pytest 17, tsc, vitest 91/91,
    vite build — зелёные. Push — не выполнялся pipeline'ом (master пользователем сам
    запушен до 4a6e048; merge'и — локально).  - **2026-10-09** — VAD: Silero (onnx) в batch-пути (T-20261008211334 R1): см. решение #40.
    Одна активная роль (backend, ветка agent/backend/2): ADR-002 поправку (выбор варианта 2
    со сравнением) и docs worker выполнил сам; код — backend-агент (b3cd124 voice, 1089c4b
    api+отчёт). Worker при интеграции: вычистил устаревший комментарий isSpeaking (4101f66),
    сделал gen_fixtures самодостаточным (речь из закоммиченной фикстуры, FOR_RUN — только
    fallback первого генерирования) + обновил speech_quiet.pcm (b3763fc). Merge в master:
    dee864b (no-ff). Независимый прогон measure_vad.py подтвердил таблицы (а)/(б): ложные
    energy 1 сег/3008 мс → silero 0 (noise, breathing), честная/тихая речь оба ловят,
    latency конца 908→608 мс. Регресс (fresh, master): go vet+test (httpapi/voicesvc/vad,
    sandbox), CGO_ENABLED=0 build, pytest 27 (17→+10), tsc, vitest 91/91, vite build —
    зелёные. Push — не выполнялся.
- **2026-10-09** — Детерминированный A/B-замер голосового пайплайна (T-20261009001516 R1):
  см. решение #41. Реализовано: env-mock `LLM_MOCK_RESPONSE`/`LLM_MOCK_TOKENS_PER_S` +
  `mock.SetTokenDelay` (efe318c, 4 теста) — детерминизация LLM для замеров; детерминированный
  пробер `services/voice/latency/probe_deterministic.py` (фикс. реплика кандидата, отсчёт
  «конец речи → первый звук ИИ», deкомпозиция stt/llm-tts, N=8, p50/p95, JSON); A/B
  BEFORE(4a6e048+env-mock)/AFTER(HEAD) на общем voice large-v3 CUDA (worktree в /tmp, убран
  после). Итог: first_tts p50 1.46→1.23 с, p95 1.65→1.37 с; чистый эффект clause-dispatch
  (llm/tts) 0.59→0.15 с; SLO p95<4 с выполняется; второй узел — STT (не clause-dispatch).
  Коммит a163bcf (пробер + отчёт docs/test-results/latency-deterministic-2026-10-09.md).
  Регресс: go vet+test api (все пакеты) + sandbox, tsc, vitest 91/91, vite build, pytest 27,
  gofmt — зелёные. Push — не выполнялся.
- **2026-10-09** — Наблюдаемость barge-in: pre-roll-учёт + дашборд (T-20261009021739 R1):
  см. решение #42. Вариант (b) — простой pre-roll-учёт (VADStream.PrerollMS →
  completeUtterance: ms = totalMS − prerollMS, порог 500 мс не тронут) + метрика
  grade_barge_in_speech_ms (3 точки barge-in) + Grafana v2 (row Barge-in & Fallback / Latency
  +TTS synth / Health) + алертные пороги OPERATIONS. Known limitations: race кадр-триггера
  (занижение ≤750 мс, безопасное направление) + post-silence (завышение ≤ ~500 мс) — в ADR-002.
  Тест TestBargeInSpeechMSBuckets (детерминизм: приминг-кадр + дельта fallback-метрики +
  кадры 50 мс). Коммиты cb7e542 (код+тест), 9ba4cc4 (дашборд), 75bfe31 (доки). Регресс:
  go vet+test api (все пакеты, httpapi 64.8 с) + sandbox, gofmt -l пусто, tsc, vitest 91/91,
  vite build, pytest voice 27 — зелёные. Mock-scrape /metrics: grade_barge_in_speech_ms
  в экспорте. Docker недоступен — monitoring-профиль не поднимался. Push — не выполнялся.
- **2026-10-09** — A/B промптов ИИ-интервьюера (T-20261009121144 R1):
  см. решение #43. Победитель B (voice-формат-блок: ≤40 слов, 1–2 предложения,
  реакция + один вопрос/подсказка, без списков/водных) — `activeVoiceStyle`.
  LLM-judge на живых ответах (qwen3.8-27b-fp8): A=18.1 vs B=18.9 (макс 20, 7 voice-
  сценариев; junior: A 41–62 слова → B 11–24, grade_fit 3→5; middle/senior — ничьи).
  Latency probe_deterministic (реальный LLM, N=5): first_tts p50 2.921 (A) vs 2.393 (B) —
  нейтрально. Детерминированные тесты контракта + эвристики; TestABEvalLLM — opt-in
  (LLM_EVAL=1, enable_thinking=false). Полный сьют зелёный (go vet+test api/sandbox,
  gofmt -l, tsc, vitest 91/91, vite build, pytest voice 27). Push — не выполнялся.
- **2026-10-09** — Prod-гигиена: sandbox-изоляция, CI docker-пробы, prod-compose
  (T-20261009133832 R1): см. решение #44. RLIMIT subprocess-раннера (RLIMIT_AS
  go 2 ГБ VA / python 512 МБ + RLIMIT_CPU 10 с, sh-proлог ulimit — stdlib
  SysProcAttr без Rlimit) + маркировка исчерпания лимитов (128+SIGXCPU/137/SIGXCPU
  → 124/timeout). Тесты: память 2 ГБ → MemoryError 0.4 с, вечный цикл (CPU 2 с)
  → ~2 с 124/timeout, банк 12 задач — регресс зелёный. CI: джоб sandbox-docker +
  TestSandboxDockerProbe (opt-in SANDBOX_DOCKER=1: go-pass+network=none, py-pass,
  py-oom 137) + scripts/ci-docker-test.sh. Prod-compose: sandbox.Dockerfile +docker-cli
  (без него prod всегда 503), Caddyfile tls-фикс (letsencrypt — невалидный аргумент),
  .env.example (VITE_MIC_DEBUG/STT_DOWNLOAD_ROOT/−STT_COMPUTE_TYPE), compose +TTS_SPEAKER.
  ADR-003 поправка, TEST_PLAN §4, OPERATIONS §1 чек-лист. Docker недоступен —
  валидация структуры. Полный сьют зелёный. Push — не выполнялся.
