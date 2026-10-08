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
  дашборд **Grade → «Grade — голосовой контур (voice SLO)»**:
  p50/p95 `grade_ai_turn_seconds` по стадиям (ключевой SLO — p95
  `llm_first_token` < 4 с), rate ходов/мин, ошибки (LLM-стрим, TTS).
- Prometheus: `http://<vps>:9090` (скрейп `api:8000/metrics` + self, retention 7 д).

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
