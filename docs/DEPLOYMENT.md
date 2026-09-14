# Деплой и эксплуатация «Грейд» (Фаза 5)

**Статус:** v1.0 (2026-09-14). CI (задача 1) — выполнен (`.github/workflows/ci.yml`);
продакшен-среда (задача 2) и мониторинг (задача 3) — план и артефакты готовы,
развёртывание требует VPS/cloud-узел пользователя (phase gate).

## 1. CI/CD (выполнено)

`.github/workflows/ci.yml` — на push в master и PR (concurrency-отмена старых):

| Job | Что | Время |
|---|---|---|
| go | api + sandbox: gofmt-вет, go vet, go test (load-тест входит), CGO_ENABLED=0 build | ~10 мин |
| frontend | pnpm install --frozen-lockfile, tsc --noEmit, vitest run, vite build | ~5 мин |
| voice | python 3.12 venv, torch CPU ПЕРЕД requirements (иначе nvidia-* ~2 ГБ), pytest с fake-провайдерами (детерминизм, без ML-моделей) | ~15 мин |
| smoke | сборка api → запуск (LLM_MOCK, sqlite, :8877) → scripts/smoke.sh (полный REST-контур: регистрация → сессия → report 409/202→200 → кабинет) | ~5 мин |

Все jobs проверены локальной эмуляцией (SMOKE OK, pytest 12 passed).

**CD (деплой на узел)**: артефактный — compose-сборка на целевом узле:
`make up` (prod: api+frontend+voice+sandbox+postgres) / `make up-gpu` (voice на
GPU-узле). Автодеплой по тегу (docker registry + ansible/ssh) — добавляется
после появления prod-узла (зависимость задачи 2).

## 1.1 Скачивание моделей (scripts/download-models.sh)

Модели скачиваются в `MODELS_DIR` (default `/mnt/models`):

| Модель | Директория | Размер | Ссылка |
|--------|------------|--------|--------|
| **faster-whisper small** (STT) | `$MODELS_DIR/stt/Systran/faster-whisper-small/` | ~460 МБ | https://huggingface.co/Systran/faster-whisper-small |
| **Silero TTS v5 RU** (TTS) | `$MODELS_DIR/tts/silero-tts-v5_ru.pt` | ~150 МБ | https://models.silero.ai/models/tts/ru/v5_ru.pt |
| **Qwen3.8-27B-Instruct** (LLM) | `$MODELS_DIR/llm/` | ~60 ГБ (fp16) | https://huggingface.co/Qwen/Qwen3.8-27B-Instruct |
| **Silero VAD onnx** | В комплекте с faster-whisper | ~2 МБ | — (не скачивается) |

**Запуск:**
```bash
# Создать директорию (нужны права):
sudo mkdir -p /mnt/models && sudo chown $USER /mnt/models

# Скачать (STT_MODEL=small по умолчанию; LLM можно пропустить):
make models
# или:
MODELS_DIR=/mnt/models STT_MODEL=small bash scripts/download-models.sh
```

**Переменные окружения для запуска:**
```bash
export STT_DOWNLOAD_ROOT=/mnt/models/stt   # faster-whisper: кэш моделей
export TTS_MODEL_DIR=/mnt/models/tts       # Silero TTS: директория модели
# LLM: vLLM скачивает из HF-кэша (HF_HOME или --local-dir); см. ADR-005.
```

## 2. Продакшен-среда (план; нужен узел)

Требования (NFR/SRS):
- **Узел app**: VPS 4 vCPU / 8 ГБ / Ubuntu 24.04 — docker + compose (prod):
  api (:8000), sandbox (:8200, docker-демон + docker.sock — ADR-003),
  voice (:8100; CPU small или GPU-узел), frontend (nginx, :8080), postgres 16.
- **LLM-узел**: vLLM + Qwen3.8-27B (OpenAI-совместимый API, ADR-005),
  1 GPU 24+ ГБ; LLM_BASE_URL — в .env api.
- **Домен + TLS**: reverse-proxy (nginx/caddy) → api (:8000, включая /ws —
  Upgrade-заголовки) и frontend; TLS-сертификат (Let's Encrypt). Медиа-потоки —
  WS-бинарные кадры (PCM16) внутри TLS — отдельный транспорт не требуется.
- **Переменные**: `.env.example` — полный справочник; в prod: JWT_SECRET
  (случайный, ≥32 байт), STT_MODEL (small CPU / large-v3 GPU),
  SANDBOX_MODE=docker, LLM_MOCK=0, LOG_LEVEL=info.

Проверочный чеклист развёртывания: `make smoke` на prod-адресе (API=https://…) —
REST-контур; ручной прогон сессии (голос: latency по TEST_PLAN §3; live-code:
/runs; design: whiteboard; finish → отчёт).

## 3. Мониторинг, алерты, логи (ключевой SLO — latency голосового контура)

**Логи**: api — slog JSON (stderr) → docker logging driver (json-file, ротация
max-size=10m × 5). Ключевые события (уже пишутся): `stt: реплика кандидата`
(конфиденция), `tts: синтез не удался` (деградация), `vad: реплика завершена`,
`доставлен лимит времени сессии`, `ws: клиент подключился/отключился`,
`отчёт готов`.

**Метрики (SLO, ARCHITECTURE §8)**:
| Метрика | Источник | Цель |
|---|---|---|
| p95 latency «речь кандидата → первый TTS-кадр» | instrumentation: метки в handleVoiceUtterance/streamAIAudio (бэклог: /metrics Prometheus) | < 4 с (с LLM) |
| Конфиденция STT (mean) | `stt: реплика кандидата` (conf) | ≥ 0.7 (small CPU) |
| Деградации voice (TTS/STT ошибок) | warn-строки | алерт при > 5/час |
| Таймер-тики без срывов | load-тест (CI) + runtime (бэклог) | 1 tick/5 с |
| /healthz + voice /api/v1/health | uptime-karma / blackbox | 200 |

**Alerting (после развёртывания)**: blackbox-проверки healthz (30 с),
алерт-правила по grep-метрикам логов (TTS-ошибки, 5xx), page-deputy по
критичным (БД недоступна, voice down > 1 мин).

**Баг-находки фаз, влияющие на prod**: (1) TTS-кадры без real-time pacing
(burst) — воспроизведение корректное, но стриминг TTS потребует pacing;
(2) стриминговый STT — путь к SLO p95<4 с на CPU (сейчас 6.5 с, см.
docs/test-results/voice-latency-2026-09-14.md); (3) GPU-конфиг (large-v3) —
качество 0.87 → 0.9+.
