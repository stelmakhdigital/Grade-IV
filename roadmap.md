# roadmap.md — фазы и задачи проекта «Грейд»

> Статусы: `[ ]` не начато · `[~]` в процессе · `[x]` выполнено.
> Обновляется при выполнении работ — после коммита (см. AGENTS.md, раздел 6).

## Фаза 0: Discovery — `[x]` ЗАВЕРШЕНА (2026-09-09, phase gate пройден)
- [x] Инициализация проекта: `AGENTS.md`, `PROJECT_MEMORY.md`, `roadmap.md`
- [x] Аудит существующих артефактов (лендинг «Грейд»: грейды, стеки, блоки программы)
- [x] Черновик архитектуры `ARCHITECTURE.md` v0.1 (Mermaid-диаграмма)
- [x] Проведён опрос по сбору требований, ответы зафиксированы (2026-09-09, решения — в PROJECT_MEMORY.md)
- [x] Определить рамки MVP и целевую аудиторию: все стадии (голос, Live-Code, System Design, отчёт), Go + Python
- [x] Уточнить терминологию: «RTS» = STT/ASR; «РМВОК» = PMBOK
- [x] Позиционирование: чистое ИИ мок-интервью (лендинг — только пример, человек из контура выведен)
- [x] Подтверждено: платформа — Web (браузер); голосовой стек — self-hosted (faster-whisper large-v3-russian GPU, Silero v5 MIT; /api/v1/stt, /api/v1/tts + абстракция провайдера)
- [x] Итог Discovery (цели, критерии успеха, допущения, риски) — в PROJECT_MEMORY.md; phase gate — одобрение пользователя 2026-09-09

## Фаза 1: Requirements — `[x]` ЗАВЕРШЕНА (2026-09-09, phase gate пройден)
- [x] User stories и сценарии сессии (US-1…US-10, MoSCoW)
- [x] Функциональные требования: FR-A/S/V/C/R/B (кабинет, сессия, голос, сандбокс, отчёт, тарификация)
- [x] Нефункциональные требования: NFR-1…NFR-10 (latency, надёжность, безопасность, данные, лицензии)
- [x] Критерии приёмки по каждому user story + определение «минуты интервью» (R8 закрыт)
- [x] Спецификация требований: REQUIREMENTS.md (SRS v1.0-черновик)
- [x] SRS v1.0 одобрено пользователем (phase gate, 2026-09-09)

## Фаза 2: Design (архитектура) — `[x]` ЗАВЕРШЕНА (2026-09-09, phase gate пройден)
- [x] ADR-001…005: транспорт (WS + PCM16 16 кГц), голосовой конвейер (VAD/STT/LLM/TTS + latency-бюджет), сандбокс (Docker на сессию), whiteboard (Excalidraw + палитра 12 блоков), сервинг LLM (vLLM/llama.cpp, OpenAI-совместимо) — `docs/adr/`
- [x] Финальная архитектура: компоненты, данные, последовательности (ARCHITECTURE.md v0.3)
- [x] Mermaid: компоненты + sequence (голосовой ход, Live-Code, System Design)
- [x] Модель данных (7 таблиц) и контракты API (REST + WS + /api/v1/stt|tts + sandbox)
- [x] Метрики качества: latency-бюджет по этапам (ADR-002), метрики Prometheus + алерты (§6 v0.3)
- [x] SRS v1.1: критерии и веса отчёта по грейдам (REQUIREMENTS.md §12)
- [x] WBS Implementation: WP-1…WP-12 (Фаза 3)
- [x] Review архитектуры с пользователем (phase gate, 2026-09-09; правка «бекэнд = Go» — ADR-006, v0.4)

## Фаза 3: Implementation — ЗАВЕРШЕНА (WP-1…WP-12, коммиты 2026-09-13…14; phase gate: одобрение пользователя перед Фазой 4)
- [x] WP-1: Каркас: services/api (Go), services/sandbox (Go), services/voice (Python), services/frontend (Vite+React+TS), infra/, Makefile; тесты: go test + vitest + pytest
- [x] WP-2: api (Go): модель данных (7 таблиц, DDL) + аутентификация (JWT, bcrypt)
- [x] WP-3: api (Go): машина состояний сессий + WS-протокол + тарификация (pause/resume/finish, лимит 60 мин, таймер) — коммит 2026-09-13
- [x] WP-4: voice (Python): /api/v1/stt (faster-whisper), /api/v1/tts (Silero v5), абстракция провайдеров, health — коммит 2026-09-14
- [x] WP-5: api (Go): LLM-слой (OpenAI-совместимый клиент) + движок интервьюера (персона, промпты, рубрики, nudge, ревью кода) — коммит 2026-09-14
- [x] WP-6: sandbox (Go): Docker-runner (лимиты, --network=none) + subprocess dev-mode + банк задач Go/Python (12 задач, теги по грейдам) + api-прокси /runs (submissions, code_run, run_result по WS) — коммит 2026-09-14
- [x] WP-6a: голосовой конвейер (ADR-002) в api: PCM → энергетический VAD → /stt → движок интервьюера → /tts → бинарные кадры {seq,flags}+PCM16 (turn-taking, one-parallel-turn, graceful degradation) — коммит 2026-09-14
- [x] WP-7: Frontend: каркас Vite+React+TS, кабинет (auth, минуты, история) — коммит 2026-09-14 (e5af092)
- [x] WP-8: Frontend: голосовая сессия (AudioWorklet-микрофон → WS, PCM-воспроизведение, транскрипт, таймер) — коммит 2026-09-14 (09a4af8)
- [x] WP-8b: голосовой контур (багфиксы/доводка): эквалайзер уровня микрофона + диагностика worklet, паузы между фразами TTS, транслитерация англ. терминов для TTS, мужской голос (aidar), интонации в промпте, SESSION_LIMIT_S (снятие лимита времени сессии) — 2026-09-16
- [x] WP-9: Frontend: Live-Code (Monaco, «Запустить тесты», ИИ-ревью, follow-up) — коммит 2026-09-14 (см. git log)
- [x] WP-10: Frontend: System Design (Excalidraw + палитра 12 блоков, сохранение, ИИ-оценка) — коммит 2026-09-14 (см. git log)
- [x] WP-11: Отчёт (генерация по критериям §12, UI, история) — коммит 2026-09-14 (см. git log)
- [x] WP-12: Инфра и доки: docker-compose (profiles default/gpu), .env.example, Makefile, README, e2e-smoke скрипт — коммит 2026-09-14 (см. git log)

## Фаза 4: Testing — ЗАВЕРШЕНА (все 5 задач, коммиты 2026-09-14; phase gate: одобрение пользователя перед Фазой 5)
- [x] План тестирования (юнит, интеграция, E2E, load) — docs/TEST_PLAN.md v1.0, 2026-09-14
- [x] Тесты голосового контура: latency end-to-end, качество STT/TTS — docs/test-results/voice-latency-2026-09-14.md (probe + эталон 20, small ≥0.85), 2026-09-14
- [x] Тесты сандбокса кода (изоляция, лимиты ресурсов, доверенность тестов) — пробы subprocess + фикс бага таймаута, docs/test-results/sandbox-2026-09-14.md, 2026-09-14
- [x] Load-тест real-time нагрузки (одновременные сессии) — 50 WS-сессий в make test, отчёт docs/test-results/voice-latency-2026-09-14.md (load), 2026-09-14
- [x] Исправление багов, regression-прогон — фикс LOG_LEVEL; регресс: go vet+test, vitest ×3, build, smoke — 2026-09-14

## Фаза 5: Deployment
- [x] CI/CD (сборка, тесты, деплой) — .github/workflows/ci.yml (go/frontend/voice-fake/smoke, jobs эмулированы локально); CD — make up (артефакт), 2026-09-14
- [x] Продакшен-среда (cloud/VPS), домен, TLS, медиа-потоки — 2026-09-28 (9f18dfb): prod-compose (Caddy+TLS Let's Encrypt/internal), OPERATIONS.md; факт-деплой ждёт VPS
- [x] Мониторинг, алерты, логи (latency голосового контура — ключевой SLO) — 2026-09-28 (9f18dfb): Prometheus+Grafana, SLO-дашборд p95 llm_first_token<4с; алерт-правила — бэклог

## Фаза 6: Operations
- [x] Документация по эксплуатации — 2026-09-28 (9f18dfb): docs/OPERATIONS.md (VPS, бэкап, troubleshooting)
- [x] Локальный voice-контур (предварительная pipeline-задача) — 2026-10-05 (67e1694): STT large-v3 GPU (cuda/float16, 0.2 с/5 с аудио) + TTS Silero v5 CPU, модели в `services/voice/models/`, LLM `qwen3.8-27b-fp8`; живые пробы STT/TTS/LLM зелёные (roundtrip conf 0.857, сходство 0.846, отчёт 18 с)
- [x] Full-duplex с barge-in (a3bfc11): VAD слушает кандидата во время речи ИИ, порог 500 мс (BargeInMinSpeechMS), WS tts_stop, player.stop(), браузерный AEC, метрика grade_barge_ins_total; live-замер подтверждён
- [x] Диагностика «молчащего» микрофона (881eaae): rms чанков < 0.005 ≥ 5 с → UI-предупреждение «Микрофон молчит» + состояние muted; fallback-переход (AudioWorklet→ScriptProcessor) — info-сообщение в UI; live-проверка: мьютный вход → warning ~5–8 с, рабочий вход (реальное аудио) → ложных предупреждений нет, речь распознаётся (conf 0.845/0.93)
- [x] Пауза/возобновление сессии — полный контур (pipeline T-20261008154023 R1, 2026-10-08; merge fed3c4a/26419c4/5e2a31c): backend — pause останавливает активный TTS-стрим (tts_stop + end-кадр, реестр WS-соединений), ходы берут статус из движка (гонка закрыта), paused-сессия не обрабатывает реплики (текст + голос), resume после порога — 409+aborted; frontend — кнопки «Пауза»/«Продолжить» + баннер, mic/player стоп до REST, автовозврат мика после resume; docs — актуализация README/OPERATIONS/DEPLOYMENT/TEST_PLAN/.env.example + секция FR-S7; тесты: session_pause_test.go (5), vitest 73; скриншоты screenshots/session-pause/
- [x] Session-level стоп TTS-канала (pipeline T-20261008154023 R2, 2026-10-08; merge 02278c7/09f3b48/b43253a): `sessStopped` на wsSession (pause/finish → стоп, resume → сброс); beginTTS при флаге — закрытый канал (гонка окна закрыта); флаг во всех циклах шлюзания кадров; finish во время стрима — end-кадр + tts_stop; тесты TestSessionPauseDuringLLMPhase, TestSessionFinishDuringTTSStream; frontend — unit-тесты mic-диагностики (судья I1, vitest 82); ADR-007 поправка + README
- [x] Надёжность захвата микрофона (T-20261008175905 R1, 2026-10-08; ADR-008, merge c0d2d8a/6feab07): авторетрайт worklet→fallback→реинициализация (MAX_REINIT=2, первый чанок — сброс), muted → кликабельная диагностика /audio-debug.html, +8 тестов (91/91), скриншоты screenshots/mic-retry/, latency-проба без регрессии
- [x] Клиуза-диспетчизация TTS (T-20261008185701 R1, 2026-10-08; поправка ADR-002, merge 1378046/8ec6858): MIN_CLAUSE_CHARS=48, клиузы по «,»/«;»/«:»/«—», ранний запуск TTS на клиузе (запас ~1 с до конца предложения), gap по пунктуации («?/!» 500 мс, «.» 250 мс, клиузы 0); тесты TestSplitForTTS/TestTTSGap/TestClauseDispatchEarlyTTS; A/B без регрессии (docs/test-results/tts-clause-dispatch-2026-10-08.md)
- [ ] Бэклог улучшений: мультиязычие

## Бэклог ML/latency
- [x] Стриминг LLM (SSE) + до-стриминг TTS + real-time pacing + /metrics (grade_ai_turn_seconds{stage}) — 2026-09-28 (3cc09d8): накладка конвейера 3 мс–0.3 с; узкое место — LLM-узел (reasoning 2.7–7.9 с до первого content-токена); LLM_MODEL → qwen3.8-27b-fp8
- [x] Ускорение на стороне LLM-узла: отключение reasoning через chat_template_kwargs.enable_thinking (LLM_ENABLE_THINKING, дефолт off) — 2026-09-28 (847f319): TTFB 7.38с→0.70с
- [x] Рефакторинг долга: engine.go 651→4 файла, дубли ws-хендлеров (speak/ownedSession/parseSessionID), SessionView→хук useVoiceSession — 2026-09-28
- [x] Стриминговый STT + Silero VAD (onnx) + Pre-STT (очередь #2, T-20261005132522 R1) — 2026-10-05 (ab0bc3f/6556b72/e00b4c6/abb57f6): WS /api/v1/stt/stream (voice: Silero VAD, partial ≤1/500 мс, final по тишине 600 мс), partial → UI (интеримная строка), ход по final, barge-in и во время речи (500 мс от state=true), fallback на batch (grade_stt_stream_fallbacks_total); live: транскрипт 3-с реплики 754→695 мс (A/B), partial за 2.1 с до конца речи, ложных final на тишине нет; ADR-007
- [x] Параллельный TTS-синтез (pipeline, очередь #3, T-20261005184303 R1) — 2026-10-06 (f2aa87e/94d45e9): пул TTSParallelism=3 (stdlib goroutine+channel) в streamCandidateTurn — предложение N+1/N+2 синтезируется, пока N уходит по pacing; порядок кадров сохранён (реордер по idx), barge-in (недопущенные не стартуют) и деградация (ошибка→skip) без изменений; метрика grade_ai_tts_synth_seconds (длительность на предложение); live A/B: first_tts 388/435/730→367/359/433 мс (в пределах LLM-шума), межфразовые разрывы ~без изменений (750–1500 мс) — узкое место LLM-стриминг, синтез ~89 мс/предложение (не bottleneck)
- [x] VAD: Silero (onnx) вместо энергетического в batch-пути (T-20261008211334 R1, 2026-10-09; поправка ADR-002 вариант 2, merge dee864b): voice — `WS /api/v1/vad/stream` (Silero VAD, state-события start/pre_silence/end, pre_silence 400 мс — сигнал pre-STT) + StreamVAD.pre_silence_ms (дефолт None, /stt/stream без изменений); api — `VADStream` (тонкий клиент: pre-ring-буфер + pre_silence-снимок + utterance), диспетчер feedVAD 3 уровня (/stt/stream → /vad/stream+batch /stt → energy VAD last-resort, не удалён), DRY preSTTFor/completeUtterance, isSpeaking() анти-nudge, метрика grade_vad_stream_fallbacks_total; CGO_ENABLED=0 сохранён (onnx только в Python); фикстуры (noise/breathing/quiet/honest speech) + замер: ложные реплики energy 1/3008 мс → silero 0 (noise, breathing), честная/тихая речь оба ловят (регрессии нет), latency конца 908→608 мс; тесты: voicesvc VADStream, httpapi VADStream (pre-STT/barge-in/fallback), voice test_vad_stream.py (10); отчёт docs/test-results/vad-silero-batch-2026-10-09.md
- [x] Детерминированный A/B-замер голосового пайплайна (T-20261009001516 R1, 2026-10-09; merge a163bcf + env-mock efe318c): закрыт открытый вопрос о влиянии clause-dispatch (#39) на p95. Метод: фиксированный LLM-мок (`LLM_MOCK_RESPONSE`+`LLM_MOCK_TOKENS_PER_S`+`mock.SetTokenDelay`) устраняет LLM-недетерминизм; детерминированный пробер `services/voice/latency/probe_deterministic.py` (фикс. реплика кандидата 1.95 с, отсчёт «конец речи → первый звук ИИ», deкомпозиция stt/llm-tts + межфразовые паузы, N=8, p50/p95, JSON). A/B BEFORE(4a6e048+env-mock)/AFTER(HEAD) на общем voice large-v3 CUDA (worktree /tmp, убран после): first_tts p50 1.46→1.23 с (−0.23), p95 1.65→1.37 с (−0.28); чистый эффект clause-dispatch (llm/tts, транскрипт→1-й кадр) 0.59→0.15 с (−0.44); SLO p95<4 с выполняется (AFTER 1.37 с); второй узел — STT (VAD 600 мс + large-v3), не clause-dispatch; trade-off turn_end +1.37 с (7 клиузных пауз vs 3). Silero VAD batch (#40) — fallback-путь, в этом A/B не exercised (измерен в решении #40). docs/test-results/latency-deterministic-2026-10-09.md
- [x] Наблюдаемость barge-in: pre-roll-учёт + дашборд голосового контура (T-20261009021739 R1, 2026-10-09; коммиты cb7e542/9ba4cc4/75bfe31; ADR-002 поправка): закрыт риск #40 (pre-roll ≤1 с входил в barge-in-длительность по байтам). Вариант (b) — простой pre-roll-учёт: `VADEvent.PreRollMS` (байты pre-ring при старте речи), `completeUtterance(ws, utterance, prerollMS)`: порог BargeInMinSpeechMS=500 (не тронут) против `ms = totalMS − prerollMS`; energy-путь prerollMS=0 (без изменений); стрим-путь не тронут. Метрика `grade_barge_in_speech_ms` (histogram, мс, бакеты 100/250/500/1000/2000/5000) в 3 точках barge-in. Grafana v2: row «Barge-in & Fallback» (rate/мин, доля ≤500 мс, fallback-счётчики, speech_ms p50/p95), «Latency» (+grade_ai_tts_synth_seconds), «Health»; алертные пороги — OPERATIONS §2. Known limitations (ADR-002): race кадр-триггера (занижение ≤750 мс, безопасное направление) + post-silence (завышение ≤~500 мс). Тест TestBargeInSpeechMSBuckets (Silero-путь: ms=1250 ∈ (1000,2000], длинная речь прерывает TTS); регресс TestBargeIn_*/TestVADStream_* зелёные
- [x] A/B промптов ИИ-интервьюера (T-20261009121144 R1, 2026-10-09; docs/test-results/prompts-ab-2026-10-09.md): победитель B — voice-формат-блок (1–2 предложения, ≤ 40 слов; реакция + один вопрос/подсказка; без списков/«давайте разберём») зафиксирован `activeVoiceStyle=voiceStyleB` (константы A/B + билдер `systemPrompt(grade, stack, stage, voiceStyle)`); персона/программы/LiveCode/Design не тронуты. Оценка: детерминированные (контракт промптов + эвристика реплик: TestPromptABContract/TestVoiceReplyHeuristic/TestPromptSmokeMock/TestCodeRunHintContract) + LLM-judge на живых ответах qwen3.8-27b-fp8 (4 критерия 1–5, корпус 10 сценариев): A=18.1 vs B=18.9, B не хуже ни в одном, строго лучше в 3 junior (A: 41–62 слова → B: 11–24, grade_fit 3→5); latency probe_deterministic (реальный LLM, N=5): first_tts p50 2.921 (A) vs 2.393 (B) — нейтрально. TestABEvalLLM — opt-in (LLM_EVAL=1, enable_thinking=false как в проде)

## Риски (предварительный список, обновлять по фазам)
- R1: Latency голосового контура (STT+LLM+TTS) — превысит приемлемые 2–3 с (MVP), >1 с (цель)
- R2: Безопасность сандбокса выполнения кода пользователей
- R3: Стоимость cloud STT/TTS/LLM при масштабировании
- R4: Размытие рамки MVP (System Design — самая «размазистая» стадия)
- R5: Непонимание/промах в STT на живом голосе кандидатов (акценты, шум)
- R6: Разрыв позиционирования: лендинг обещает живого инженера — **закрыт** (2026-09-09): лендинг — только пример, продукт — чистое ИИ-интервью
- R7: Планка «живого диалога» (интонации, barge-in, заполнение пауз) — самый рискованный технически пункт; смягчение: streaming-конвейер STT/LLM/TTS, поведенческий промпт LLM, VAD-таймер молчания
- R8: Поминутная тарификация — **закрыт** (2026-09-09): определение «минуты интервью» в REQUIREMENTS.md, раздел 7
