#!/usr/bin/env bash
# Скачивание моделей для системы «Грейд» в MODELS_DIR (default /mnt/models).
#
# Что скачивается:
#   stt/  — faster-whisper (Systran/faster-whisper-{STT_MODEL}, default small)
#   tts/  — Silero TTS v5 RU (silero-tts-v5_ru.pt)
#   llm/  — Qwen3.8-27B-Instruct (для vLLM; ~60 ГБ fp16 / ~30 ГБ int8)
#
# VAD (silero_vad_v6.onnx) — в комплекте с faster-whisper (не скачивается).
#
# Использование:
#   bash scripts/download-models.sh                  # /mnt/models
#   MODELS_DIR=/data/models bash scripts/download-models.sh
#   STT_MODEL=large-v3 LLM_MODEL=Qwen/Qwen3.8-27B-Instruct bash scripts/download-models.sh
#
# Нужны: curl, python3 (для huggingface_hub; voice-venv используется если есть).
set -euo pipefail

MODELS_DIR="${MODELS_DIR:-/mnt/models}"
STT_MODEL="${STT_MODEL:-small}"
TTS_MODEL_FILE="silero-tts-v5_ru.pt"
TTS_URL="https://models.silero.ai/models/tts/ru/v5_ru.pt"
LLM_MODEL="${LLM_MODEL:-Qwen/Qwen3.8-27B-Instruct}"

echo "=== Целевая дирекория: $MODELS_DIR ==="
mkdir -p "$MODELS_DIR/stt" "$MODELS_DIR/tts" "$MODELS_DIR/llm"

# --- 1) STT: faster-whisper (HuggingFace) ---
echo ""
echo "=== [1/3] STT: faster-whisper $STT_MODEL (~$([ "$STT_MODEL" = "large-v3" ] && echo "3 ГБ" || echo "460 МБ")) ==="
if [ -d "$MODELS_DIR/stt/Systran/faster-whisper-$STT_MODEL" ]; then
  echo "  Уже существует: $MODELS_DIR/stt/Systran/faster-whisper-$STT_MODEL"
else
  # Используем voice-venv (там huggingface_hub) или ставим временно.
  VENV_PY="services/voice/.venv/bin/python"
  if [ ! -f "$VENV_PY" ]; then
    echo "  Ошибка: нет voice-venv (make install). Ставим huggingface_hub во временный venv."
    python3 -m venv /tmp/hf-venv
    /tmp/hf-venv/bin/pip install -q huggingface_hub
    VENV_PY="/tmp/hf-venv/bin/python"
  fi
  "$VENV_PY" - <<PY
import os
from huggingface_hub import snapshot_download
path = snapshot_download(
    repo_id="Systran/faster-whisper-$STT_MODEL",
    local_dir="$MODELS_DIR/stt/Systran/faster-whisper-$STT_MODEL",
)
print(f"  Скачано: {path}")
PY
fi

# --- 2) TTS: Silero v5 RU ---
echo ""
echo "=== [2/3] TTS: Silero v5 RU (~150 МБ) ==="
if [ -f "$MODELS_DIR/tts/$TTS_MODEL_FILE" ]; then
  echo "  Уже существует: $MODELS_DIR/tts/$TTS_MODEL_FILE"
else
  curl -L --progress-bar -o "$MODELS_DIR/tts/$TTS_MODEL_FILE.part" "$TTS_URL"
  mv "$MODELS_DIR/tts/$TTS_MODEL_FILE.part" "$MODELS_DIR/tts/$TTS_MODEL_FILE"
  echo "  Скачано: $MODELS_DIR/tts/$TTS_MODEL_FILE"
fi

# --- 3) LLM: Qwen3.8-27B-Instruct (HuggingFace, для vLLM) ---
echo ""
echo "=== [3/3] LLM: $LLM_MODEL (~60 ГБ fp16) ==="
echo "  (Это большая загрузка. Для CPU-демо можно пропустить: LLM_MOCK=1.)"
read -p "  Скачать LLM-модель? [y/N] " -r
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
  echo "  Пропущено (LLM_MOCK=1 для демо)."
  exit 0
fi
# huggingface-cli (если есть) или python-скрипт.
if command -v huggingface-cli &> /dev/null; then
  huggingface-cli download "$LLM_MODEL" --local-dir "$MODELS_DIR/llm"
else
  "$VENV_PY" - <<PY
from huggingface_hub import snapshot_download
path = snapshot_download(
    repo_id="$LLM_MODEL",
    local_dir="$MODELS_DIR/llm",
)
print(f"  Скачано: {path}")
PY
fi

echo ""
echo "=== Готово ==="
echo ""
echo "Теперь задайте в .env (или окружении):"
echo "  STT_DOWNLOAD_ROOT=$MODELS_DIR/stt"
echo "  TTS_MODEL_DIR=$MODELS_DIR/tts"
echo "  LLM_BASE_URL=http://LLM_NODE:8001/v1  (если vLLM на отдельном узле)"
echo ""
echo "Пример запуска (CPU-демо без LLM):"
echo "  export LLM_MOCK=1"
echo "  export VOICE_URL=http://127.0.0.1:8100"
echo "  export SANDBOX_URL=http://127.0.0.1:8200"
echo "  export STT_DOWNLOAD_ROOT=$MODELS_DIR/stt"
echo "  export TTS_MODEL_DIR=$MODELS_DIR/tts"
echo "  cd services/api && go run ./cmd/api"
