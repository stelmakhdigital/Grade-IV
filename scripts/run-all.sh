#!/usr/bin/env bash
# run-all.sh — запуск/остановка всего стека «Грейд» одной командой.
#
# Использование:
#   bash scripts/run-all.sh start   # построить и запустить все 4 сервиса
#   bash scripts/run-all.sh stop    # остановить все
#   bash scripts/run-all.sh status  # показать состояние
#   bash scripts/run-all.sh restart # stop + start
#
# Переменные окружения (опционально, есть дефолты):
#   LLM_BASE_URL  (дефолт http://192.168.1.114:8000/v1) — LLM-узел (vLLM)
#   LLM_MODEL     (дефолт qwen3.8-27b-dflash2)
#   LLM_MOCK      (дефолт 0; 1 — без LLM-узла, эхо-интервьюер)
#   STT_MODEL     (дефолт small)
#
# Файлы: БД и PID/логи — в FOR_RUN/ (вне git). Модели должны быть скачаны:
#   bash scripts/download-models.sh   (FOR_RUN/stt, FOR_RUN/tts)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUN="$ROOT/FOR_RUN"
PIDS="$RUN/pids"
LOGS="$RUN/logs"
DB="$RUN/run.db"
mkdir -p "$PIDS" "$LOGS" "$RUN/sbxw"
export PATH="$PATH:$HOME/.local/go-toolchain/bin"

LLM_BASE_URL="${LLM_BASE_URL:-http://192.168.1.114:8000/v1}"
LLM_MODEL="${LLM_MODEL:-qwen3.8-27b-dflash2}"
LLM_MOCK="${LLM_MOCK:-0}"
STT_MODEL="${STT_MODEL:-small}"
VOICE_URL="http://127.0.0.1:8100"
SANDBOX_URL="http://127.0.0.1:8200"

API_BIN="$RUN/bin/grade-api"
SBX_BIN="$RUN/bin/grade-sbx"

is_running() { # $1 = имя (пид-файл)
  local f="$PIDS/$1.pid"
  [[ -f "$f" ]] && kill -0 "$(cat "$f")" 2>/dev/null
}

stop_one() {
  local name="$1" f="$PIDS/$1.pid" port pids p
  case "$name" in
    sandbox) port=8200 ;; voice) port=8100 ;; api) port=8000 ;; frontend) port=5173 ;;
  esac
  if is_running "$name"; then
    kill "$(cat "$f")" 2>/dev/null || true
  fi
  # подстраховка: процесс по порту (pid в файле может быть обёрткой)
  pids=$(ss -tlnp 2>/dev/null | grep -E "[:.]$port([[:space:]]|$)" | grep -oE "pid=[0-9]+" | cut -d= -f2 | sort -u)
  for p in $pids; do kill "$p" 2>/dev/null || true; done
  sleep 1
  rm -f "$f"
}

do_stop() {
  for n in frontend api voice sandbox; do stop_one "$n"; done
  echo "остановлено"
}

do_build() {
  mkdir -p "$RUN/bin"
  echo "==> сборка api"
  (cd "$ROOT/services/api" && go build -o "$API_BIN" ./cmd/api)
  echo "==> сборка sandbox"
  (cd "$ROOT/services/sandbox" && go build -o "$SBX_BIN" ./cmd/sandbox)
  if [[ ! -x "$ROOT/services/frontend/node_modules/.bin/vite" ]]; then
    echo "==> pnpm install (frontend)"
    (cd "$ROOT/services/frontend" && pnpm install --frozen-lockfile)
  fi
}

do_start() {
  for n in sandbox voice api frontend; do
    if is_running "$n"; then
      echo "!! $n уже запущен (pid $(cat "$PIDS/$n.pid")) — пропускаю (make stop для перезапуска)"
    fi
  done
  do_build

  echo "==> sandbox :8200"
  (cd "$ROOT" && SANDBOX_MODE=subprocess SANDBOX_WORKDIR_BASE="$RUN/sbxw" \
    "$SBX_BIN" >>"$LOGS/sandbox.log" 2>&1 & nohup "$SBX_BIN" >>"$LOGS/sandbox.log" 2>&1 & echo $! >"$PIDS/sandbox.pid")

  echo "==> voice :8100 (модели: $RUN/stt, $RUN/tts)"
  if [[ ! -d "$RUN/stt" || ! -f "$RUN/tts/silero-tts-v5_ru.pt" ]]; then
    echo "!! модели не найдены — сначала: bash scripts/download-models.sh" >&2
  fi
  (cd "$ROOT/services/voice" && STT_MODEL="$STT_MODEL" STT_DEVICE=cpu \
    STT_DOWNLOAD_ROOT="$RUN/stt" TTS_MODEL_DIR="$RUN/tts" \
    nohup .venv/bin/uvicorn app.main:app --host 127.0.0.1 --port 8100 >>"$LOGS/voice.log" 2>&1 & \
    echo $! >"$PIDS/voice.pid")

  echo "==> api :8000 (LLM_MOCK=$LLM_MOCK LLM=$LLM_BASE_URL/$LLM_MODEL)"
  (cd "$ROOT" && DATABASE_URL="sqlite://$DB" \
    JWT_SECRET="${JWT_SECRET:-grade-run-secret}" MINUTES_FREE_S=3600 \
    LLM_MOCK="$LLM_MOCK" LLM_BASE_URL="$LLM_BASE_URL" LLM_MODEL="$LLM_MODEL" \
    VOICE_URL="$VOICE_URL" SANDBOX_URL="$SANDBOX_URL" ADDR=:8000 \
    nohup "$API_BIN" >>"$LOGS/api.log" 2>&1 & echo $! >"$PIDS/api.pid")

  echo "==> frontend :5173"
  (cd "$ROOT/services/frontend" && nohup node_modules/.bin/vite --host 127.0.0.1 --port 5173 \
    >>"$LOGS/frontend.log" 2>&1 & echo $! >"$PIDS/frontend.pid")

  # ожидание готовности (до 60 с)
  for i in $(seq 1 60); do
    ok=1
    curl -sf -o /dev/null http://127.0.0.1:8200/healthz || ok=0
    curl -sf -o /dev/null http://127.0.0.1:8100/api/v1/health || ok=0
    curl -sf -o /dev/null http://127.0.0.1:8000/healthz || ok=0
    curl -sf -o /dev/null http://127.0.0.1:5173/ || ok=0
    [[ $ok == 1 ]] && break
    sleep 1
  done

  echo
  echo "Стек запущен:"
  do_status
  echo
  echo "  браузер:  http://127.0.0.1:5173"
  echo "  логи:     $LOGS/{sandbox,voice,api,frontend}.log"
  echo "  остановить: bash scripts/run-all.sh stop"
}

do_status() {
  for n in sandbox voice api frontend; do
    if is_running "$n"; then
      echo "  [pid $(cat "$PIDS/$n.pid")] $n — процесс жив"
    else
      echo "  [стоп]    $n"
    fi
  done
  curl -sf -o /dev/null http://127.0.0.1:8200/healthz && echo "  :8200 sandbox healthz — OK" || echo "  :8200 sandbox healthz — НЕ ОТВЕЧАЕТ"
  curl -sf -o /dev/null http://127.0.0.1:8100/api/v1/health && echo "  :8100 voice health — OK" || echo "  :8100 voice health — НЕ ОТВЕЧАЕТ"
  curl -sf -o /dev/null http://127.0.0.1:8000/healthz && echo "  :8000 api healthz — OK" || echo "  :8000 api healthz — НЕ ОТВЕЧАЕТ"
  curl -sf -o /dev/null http://127.0.0.1:5173/ && echo "  :5173 frontend — OK" || echo "  :5173 frontend — НЕ ОТВЕЧАЕТ"
}

case "${1:-start}" in
  start) do_start ;;
  stop) do_stop ;;
  status) do_status ;;
  restart) do_stop; sleep 1; do_start ;;
  *) echo "использование: $0 {start|stop|status|restart}"; exit 1 ;;
esac
