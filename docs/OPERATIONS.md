# Эксплуатация «Грейд» (docs/OPERATIONS.md)

Краткий runbook: развёртывание на VPS, мониторинг, обновление, бэкап, troubleshooting.
Подробности архитектуры — `ARCHITECTURE.md`, план CI/CD — `DEPLOYMENT.md`.

## 1. Подъём на VPS

Требуется: Ubuntu 24.04, docker + compose v2, 4 vCPU / 8 ГБ, домен с A-записью на узел.
LLM-узел (vLLM + Qwen3.8-27B, ADR-005) — отдельно, адрес в `LLM_BASE_URL`.

```bash
git clone <repo> && cd grade-iv
cp .env.example .env
# В .env: GRADE_DOMAIN, JWT_SECRET (случайный, ≥32 байта), LLM_BASE_URL,
# LLM_MOCK=0, SANDBOX_MODE=docker, STT_MODEL (small CPU / large-v3 GPU)

# ML-модели (STT/TTS) — каталог ./models в корне репо (см. DEPLOYMENT.md §1.1):
MODELS_DIR=./models bash scripts/download-models.sh   # stt/ + tts/

docker compose -f infra/docker-compose.yml --profile prod --profile monitoring up -d --build
```

Проверка: `docker compose -f infra/docker-compose.yml ps` (все healthy),
`curl -k https://$GRADE_DOMAIN/healthz` → `ok` (при `CADDY_TLS=internal` — `k`;
корневой CA: `docker compose cp caddy:/data/caddy/pki/authorities/local/root.crt .`).

Без публичного IP (локальная VPS-проверка): `GRADE_DOMAIN=localhost`,
`CADDY_TLS=internal`.

## 2. Метрики (Grafana / Prometheus)

- Grafana: `http://<vps>:3000` (admin / `GRAFANA_ADMIN_PASSWORD`),
  дашборд **Grade → «Grade — голосовой контур (voice SLO)»**: row «Barge-in &
  Fallback» (прерывания/мин, доля barge-in с речью ≤ 500 мс, fallback-счётчики,
  `grade_barge_in_speech_ms` p50/p95), row «Latency» (p50/p95 `grade_ai_turn_seconds`
  по стадиям — ключевой SLO: p95 `llm_first_token` < 4 с; `grade_ai_tts_synth_seconds`;
  ходы/мин), row «Health» (ошибки LLM-стрим/TTS).
- Prometheus: `http://<vps>:9090` (скрейп `api:8000/metrics` + self, retention 7 д).

### Алертные пороги (что считать инцидентом)

| Метрика | Порог (за 5 м) | Что это | Действие |
|---------|----------------|---------|----------|
| `grade_ai_turn_seconds` p95 `llm_first_token` | > 4 с | Нарушение SLO голосового контура | LLM-узел: GPU, vLLM-лог, очередь (Troubleshooting §6) |
| `grade_ai_llm_stream_errors_total` | rate > 1/мин | LLM-узел недоступен/ошибки | `curl $LLM_BASE_URL/v1/models` с app-узла |
| `grade_ai_tts_errors_total` | rate > 1/мин | TTS (voice) деградирует | voice-сервис: лог, GPU/CPU, Silero |
| `grade_stt_stream_fallbacks_total` | рост > 1 за час | /stt/stream падает → деградация на batch (latency +) | voice: /stt/stream, реконнекты, GPU |
| `grade_vad_stream_fallbacks_total` | рост > 1 за час | /vad/stream падает → energy-путь (last-resort) | voice: /vad/stream, Silero onnx |
| `grade_barge_in_speech_ms` p50 | устойчиво 500–1000 мс | Подозрение на ранние barge-in (pre-roll/post-silence, ADR-002 поправка) | Посмотреть dашборд «доля ≤ 500 мс»; при росте — пересмотр порога/подтверждения |
| `grade_barge_in_speech_ms` доля `le="500"` | > 5% за час | pre-roll-риск проявляется (прерывание на короткой речи) | Лог api: `barge-in: ... ms=... preroll_ms=...`; решение по порогу |
| `grade_barge_ins_total` | аномальный рост (×3 от базы) | Кандидат постоянно перебивает / эхо-петля | Проверить эхо (динамика+мик), TTS-громкость, barge-in-метрики |

## 3. Обновление

```bash
git pull
docker compose -f infra/docker-compose.yml --profile prod --profile monitoring up -d --build
```

Порядок: собирается image → поднимается новый контейнер → старый снимается
(`restart: unless-stopped` — автоперезапуск после сбоев/ребутов).

## 4. Бэкап БД

БД — PostgreSQL 16 (volume `pgdata`):

```bash
docker compose -f infra/docker-compose.yml exec postgres \
  pg_dump -U grade grade | gzip > backup-$(date +%F).sql.gz
# восстановление: gunzip -c backup.sql.gz | docker compose exec -T postgres psql -U grade grade
```

Рекомендуется cron-задача раз в сутки + копирование на внешнее хранилище.

## 5. Логи

```bash
docker compose -f infra/docker-compose.yml logs -f api      # slog JSON
docker compose -f infra/docker-compose.yml logs -f voice caddy
```

Ключевые события api: `stt: реплика кандидата`, `tts: синтез предложения не удался`,
`доставлен лимит времени сессии`, `отчёт готов` (список — DEPLOYMENT.md §3).

## 6. Troubleshooting

| Симптом | Причина / действие |
|---|---|
| Caddy не получает сертификат LE | A-запись домена не указывает на узел; порт 80 закрыт фаером; `docker logs caddy`; частые запросы → rate-limit LE (10/нед на домен) — для отладки `CADDY_TLS=internal` |
| `CADDY_TLS=internal`, браузер не доверяет TLS | Ожидание: импортировать корневой CA Caddy (см. §1) или проверить без `https` через `http://<vps>/` (redirect на 443) |
| LLM-узел недоступен (ошибки LLM-стрима в Grafana > 0) | `curl $LLM_BASE_URL/v1/models` с app-узла; GPU/vLLM-лог на AI-узле; метрика `grade_ai_llm_stream_errors_total` |
| voice долго отвечает (p95 > 4 с на CPU) | `STT_MODEL=small` → GPU-профиль (`--profile gpu`, large-v3) или стриминговый STT (DEPLOYMENT.md §3, баг-находка) |
| sandbox не запускает Docker-задачи | `SANDBOX_MODE=docker`, на узле работает docker-демон (mount docker.sock в compose уже есть) |
| Порт занят (80/443/3000/9090) | `ss -tlnp`; снять conflicting-сервис или поменять port-mapping в compose |
| Сессия «прервана» после долгой паузы | Ожидание (SRS §7): пауза > `SESSION_PAUSE_TIMEOUT_S` (дефолт 1800 с) → при Resume сессия `aborted`, тарифицируется фактическое активное время. Лог api: `пауза дольше порога — сессия прервана`; события сессии — GET `/api/v1/sessions/{id}/events` |

### Микрофон молчит / аудио-данные не приходят (клиент, ADR-008)

Что делает система **автоматически** (цепочка надёжности, ADR-008):
1. AudioWorklet молчит (≈ 3 с без чанков) → переход на ScriptProcessor-fallback (info-статус в UI).
2. Fallback тоже молчит → полная повторная инициализация (заново `getUserMedia`), число попыток ограничено именованной константой (~2); первый полученный чанок сбрасывает счётчик.
3. Лимит исчерпан → ошибка в UI с инструкцией открыть диагностику.
4. Вход «молчит» уровнями (rms ≈ 0 ≥ 5 с) → состояние `muted`: предупреждение «Микрофон молчит…». Пересоздания **по тишине** не делаются — браузер не различает «мёртвый путь» и реальную тишину.

Что сделать **пользователю**:
1. Проверить физическое устройство/порт, мьут на микрофоне и в ОС (выбранное устройство записи).
2. Проверить разрешение микрофона для сайта в браузере (иконка у адресной строки) — при снятом разрешении захват в состоянии `denied`.
3. Обновить вкладку (сбрасывает аудио-граф браузера) и **открыть диагностику `/audio-debug.html`**: «Проверить микрофон (5 с)» (RAW), «Проверить с AEC (как в приложении)», «Тест AudioWorklet (4 с)» (расписывает ли браузер `process()`; process=0 при живой полосе — браузер не гоняет worklet), «Тест резервного пути (6 с)», «📤 Диагностика (9 с) — отчёт агенту».
4. Ручной toggle микрофона в UI всегда доступен — авто-цепочка не блокирует ручное управление.

Что посмотреть **в логах/отчётах** (dev-инструменты, за флагами `ENABLE_DEBUG`/`VITE_MIC_DEBUG`):
- `/audio-debug.html` → кнопка «📤 Диагностика» — отчёт (RAW/AEC/worklet-сонды + события консоли) уходит POST-запросом на `http://<host>:8000/debug/mic-report` (dev-эндпоинт api; в логах появляется запись mic-report).
- Консоль вкладки приложения: `console.info` с префиксом **mic-debug** (статистика чанков/gap/rms по пути захвата) и info-сообщения о переходах (worklet → fallback → повторная инициализация).
- Dev-индикатор захвата на сессии (`debugInfo`: path, ctxState, rate, chunks, medGapMs, maxRms) — `chunks=0` при живом уровне = мёртвый путь, пересоздание должно сработать автоматически.
