#!/usr/bin/env bash
# Скачивание весов для системы «Грейд». Один запуск на свежей машине:
#
#   bash scripts/download-models.sh
#
# Файлы в git не попадают — скрипт тянет их из тех же мест, откуда их брали
# вручную, и сверяет контрольные суммы. Каталог по умолчанию — <репо>/models:
# его prod-compose монтирует в voice-контейнер как /models, а dev-стек
# (scripts/run-all.sh) сам указывает MODELS_DIR=services/voice/models.
#
# Что тянуть — переменной MODELS (по умолчанию ровно то, что нужно приложению):
#   gigaam  GigaAM v3 RNNT (~450 МБ) — STT по умолчанию (AGENTS.md)
#   tts     Silero TTS v5 RU (~145 МБ)
#   whisper faster-whisper (опция, ~460 МБ … 3 ГБ) — MODELS=whisper
#   llm     LLM для vLLM (~30-60 ГБ)                — MODELS=llm
# Пример: MODELS=gigaam,tts,whisper bash scripts/download-models.sh
#
# Переопределяется: MODELS_DIR, GIGAAM_URL (зеркало весов GigaAM), STT_MODEL
# (размер faster-whisper), LLM_MODEL, HF_REPO (зеркало репо HuggingFace).
#
# Нужны: curl, md5sum; для whisper/llm ещё python3 с huggingface_hub
# (берётся из services/voice/.venv, при его отсутствии создаётся /tmp/hf-venv).
#
# GigaAM: у Sber-CDN просрочен TLS-сертификат, поэтому зеркало через GIGAAM_URL
# обязательно, если прямой запрос падает. Список md5 — ниже; пакет gigaam сверяет
# хеш ckpt сам при загрузке; проверка здесь дублируется, чтобы обрезанный файл
# не уехал в голосовой контур.
set -euo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MODELS_DIR="${MODELS_DIR:-$REPO_DIR/models}"
MODELS="${MODELS:-gigaam,tts}"
STT_MODEL="${STT_MODEL:-small}"
LLM_MODEL="${LLM_MODEL:-Qwen/Qwen3.8-27B-Instruct}"
GIGAAM_URL="${GIGAAM_URL:-https://cdn.chatwm.opensmodel.sberdevices.ru/GigaAM}"
TTS_URL="${TTS_URL:-https://models.silero.ai/models/tts/ru/v5_ru.pt}"

# Контрольные суммы проверенных файлов. v3_e2e_rnnt.ckpt пинит и сам пакет
# (gigaam/__init__.py::_MODEL_HASHES); остальное — эталон с рабочей установки.
# Не совпало — скрипт падает и печатает фактический md5: обновить строку можно
# осознанно, а не подменяя веса молча.
MD5_v3_e2e_rnnt_ckpt=2730de7545ac43ad256485a462b0a27a
MD5_v3_e2e_rnnt_tokenizer_model=3b3bf8370e882885d79731592fc99f98
MD5_silero_tts_v5_ru_pt=b205ce893ea1ba56b0db9ee1028d9cf6

say() { printf '  %s\n' "$@"; }
fail() { printf '  ОШИБКА: %s\n' "$1" >&2; exit 1; }

# curl -> файл, затем сверка md5 по эталону (если он задан).
fetch() {
  local url="$1" dest="$2" want_md5="${3:-}"
  if [ -f "$dest" ] && { [ -z "$want_md5" ] || [ "$(md5sum "$dest" | cut -d' ' -f1)" = "$want_md5" ]; }; then
    say "уже на месте: $(basename "$dest")"
    return 0
  fi
  if [ -f "$dest" ]; then
    say "файл не по контрольной сумме, перекачиваю: $(basename "$dest")"
    rm -f "$dest"
  fi
  say "качаем: $url"
  curl -fL --progress-bar --retry 3 --retry-delay 2 --connect-timeout 20 -o "$dest.part" "$url" || fail "не скачался $url"
  mv "$dest.part" "$dest"
  if [ -n "$want_md5" ]; then
    local got
    got="$(md5sum "$dest" | cut -d' ' -f1)"
    [ "$got" = "$want_md5" ] || fail "md5 $(basename "$dest") = $got, ожидаем $want_md5"
    say "md5 совпал: $(basename "$dest")"
  fi
}

hf_download() { # repo dest
  local venv_py="$REPO_DIR/services/voice/.venv/bin/python"
  if [ ! -f "$venv_py" ]; then
    say "нет voice-venv — создаю временный с huggingface_hub"
    python3 -m venv /tmp/hf-venv
    /tmp/hf-venv/bin/pip install -q huggingface_hub || fail "не поставился huggingface_hub"
    venv_py="/tmp/hf-venv/bin/python"
  fi
  HF_REPO="$1" DEST="$2" "$venv_py" - <<'PY'
import os
from huggingface_hub import snapshot_download
print("  скачано:", snapshot_download(repo_id=os.environ["HF_REPO"], local_dir=os.environ["DEST"]))
PY
}

mkdir -p "$MODELS_DIR/stt" "$MODELS_DIR/stt-gigaam" "$MODELS_DIR/tts" "$MODELS_DIR/llm"
echo "=== Каталог: $MODELS_DIR ; забираю: $MODELS ==="
missing=()

case ",$MODELS," in
  *,gigaam,*)
    echo ""
    echo "=== STT: GigaAM v3 RNNT (дефолт проекта) ==="
    # Только та модель, на которой стоит прод: 450 МБ вместо 1.8 ГБ на все шесть
    # вариантов v3. Другие имена — через GIGAAM_MODEL + MODELS=gigaam (не пинены).
    fetch "$GIGAAM_URL/v3_e2e_rnnt.ckpt" "$MODELS_DIR/stt-gigaam/v3_e2e_rnnt.ckpt" "$MD5_v3_e2e_rnnt_ckpt"
    fetch "$GIGAAM_URL/v3_e2e_rnnt_tokenizer.model" "$MODELS_DIR/stt-gigaam/v3_e2e_rnnt_tokenizer.model" "$MD5_v3_e2e_rnnt_tokenizer_model"
    ;;
esac

case ",$MODELS," in
  *,tts,*)
    echo ""
    echo "=== TTS: Silero v5 RU ==="
    fetch "$TTS_URL" "$MODELS_DIR/tts/silero-tts-v5_ru.pt" "$MD5_silero_tts_v5_ru_pt"
    ;;
esac

case ",$MODELS," in
  *,whisper,*)
    echo ""
    echo "=== STT: faster-whisper $STT_MODEL (опция, не дефолт проекта) ==="
    if [ -d "$MODELS_DIR/stt/Systran/faster-whisper-$STT_MODEL" ]; then
      say "уже на месте: faster-whisper-$STT_MODEL"
    else
      hf_download "Systran/faster-whisper-$STT_MODEL" "$MODELS_DIR/stt/Systran/faster-whisper-$STT_MODEL"
    fi
    ;;
esac

case ",$MODELS," in
  *,llm,*)
    echo ""
    echo "=== LLM: $LLM_MODEL (десятки гигабайт) ==="
    if [ -n "$(ls -A "$MODELS_DIR/llm" 2>/dev/null)" ]; then
      say "уже непусто: $MODELS_DIR/llm"
    else
      hf_download "${HF_REPO:-$LLM_MODEL}" "$MODELS_DIR/llm"
    fi
    ;;
esac

echo ""
echo "=== Итог ==="
for p in stt-gigaam/v3_e2e_rnnt.ckpt stt-gigaam/v3_e2e_rnnt_tokenizer.model tts/silero-tts-v5_ru.pt; do
  if [ -f "$MODELS_DIR/$p" ]; then
    say "ок   $p ($(du -h "$MODELS_DIR/$p" | cut -f1))"
  elif [ "$MODELS" = "gigaam,tts" ]; then
    missing+=("$p")
  fi
done
if [ "${#missing[@]}" -gt 0 ]; then
  fail "не хватает: ${missing[*]} — без них voice молчит (провайдер выбран, весов нет)"
fi
echo ""
say "voice контейнера видит это как /models (монтируется compose), dev — через run-all.sh"
say "проверить: docker compose -f infra/docker-compose.yml --profile prod up -d voice"
say "         и GET http://127.0.0.1:8100/api/v1/health -> loaded: true"
