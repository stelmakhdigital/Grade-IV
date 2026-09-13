"""TTS-провайдер: Silero v5 (MIT, self-hosted; решение #16, ARCH §4.3).

Модель ``v5_ru`` (Silero TTS v5 для русского, 5 спикеров: aidar, baya, kseniya,
eugene, xenia). Носитель — torch-пакет с https://models.silero.ai (официальный
дистрибутив; тот же путь загрузки, что и PyPI-пакет ``silero``).
Нативные sample rates модели: 8/24/48 кГц — выход ресемплируется в 16 кГц
(контракт: PCM16 mono 16 кГц, без WAV-заголовка).

LAZY LOADING: файл модели скачивается (если ещё нет локально) и грузится при
первом запросе; singleton + lock. health до загрузки: loaded=false,
speakers — из статического описания модели.

Спикер: env ``TTS_SPEAKER`` (default ``ru_01``); если спикера нет в модели —
используется дефолтный модельный спикер + warning в лог.
"""
from __future__ import annotations

import logging
import os
import threading
import urllib.request
from pathlib import Path

from .providers import SAMPLE_RATE

logger = logging.getLogger("voice.tts")

MODEL_ID = "v5_ru"
MODEL_URL = "https://models.silero.ai/models/tts/ru/v5_ru.pt"
#: Известные спикеры модели v5_ru (для health до lazy-загрузки).
KNOWN_SPEAKERS = ("aidar", "baya", "kseniya", "eugene", "xenia")
#: Дефолтный спикер модели (default-параметр apply_tts v5).
DEFAULT_MODEL_SPEAKER = "xenia"
#: Нативный sample rate, с которого синтезируем (ближе всего к 16 кГц).
SYNTH_SAMPLE_RATE = 24000

_VOICE_ROOT = Path(__file__).resolve().parent.parent  # services/voice
DEFAULT_MODEL_DIR = _VOICE_ROOT / "models"


class TTSProviderError(RuntimeError):
    """Ошибка загрузки/синтеза TTS (эндпоинт отвечает 500 {code,message})."""


class SileroTTS:
    """Silero v5 TTS с ленивой загрузкой модели."""

    name = "silero"

    def __init__(self, model_id: str = MODEL_ID, default_speaker: str = "ru_01") -> None:
        self._model_id = model_id
        self._default_speaker = default_speaker
        self._model_path = Path(os.getenv("TTS_MODEL_PATH", "")) if os.getenv("TTS_MODEL_PATH") else (
            Path(os.getenv("TTS_MODEL_DIR", str(DEFAULT_MODEL_DIR))) / f"silero-tts-{model_id}.pt"
        )
        self._model = None
        self._lock = threading.Lock()

    # ------------------------------------------------------------------ lazy
    def _download_model(self) -> None:
        self._model_path.parent.mkdir(parents=True, exist_ok=True)
        tmp = self._model_path.with_suffix(".part")
        logger.info("Скачивание TTS-модели Silero %s (%s) -> %s", self._model_id, MODEL_URL, self._model_path)
        urllib.request.urlretrieve(MODEL_URL, tmp)  # noqa: S310 (URL фиксирован, https)
        tmp.replace(self._model_path)

    def _ensure_model(self):
        """Скачивает (при необходимости) и грузит модель один раз."""
        if self._model is None:
            with self._lock:
                if self._model is None:
                    if not self._model_path.is_file() or self._model_path.stat().st_size == 0:
                        self._download_model()
                    logger.info("Загрузка TTS-модели Silero %s", self._model_id)
                    from torch import package

                    importer = package.PackageImporter(str(self._model_path))
                    self._model = importer.load_pickle("tts_models", "model")
        return self._model

    @property
    def loaded(self) -> bool:
        return self._model is not None

    def speakers(self) -> list[str]:
        """Спикеры: из загруженной модели; до загрузки — статический список."""
        model = self._model
        if model is not None:
            return list(getattr(model, "speakers", KNOWN_SPEAKERS))
        return list(KNOWN_SPEAKERS)

    def _resolve_speaker(self, model, speaker: str) -> str:
        speakers = list(getattr(model, "speakers", KNOWN_SPEAKERS))
        if speaker in speakers:
            return speaker
        fallback = DEFAULT_MODEL_SPEAKER if DEFAULT_MODEL_SPEAKER in speakers else (
            speakers[0] if speakers else speaker
        )
        logger.warning(
            "Спикер %r отсутствует в модели Silero %s (доступны: %s); использован %r",
            speaker,
            self._model_id,
            speakers,
            fallback,
        )
        return fallback

    # --------------------------------------------------------------- contract
    def synthesize(self, text: str, speaker: str = "ru_01") -> bytes:
        """Текст -> PCM16 mono @ 16 кГц (без WAV-заголовка)."""
        if not text or not text.strip():
            raise TTSProviderError("Пустой текст")
        model = self._ensure_model()
        chosen = self._resolve_speaker(model, speaker or self._default_speaker)
        import torch
        import torch.nn.functional as F

        with torch.no_grad():
            audio = model.apply_tts(text=text, speaker=chosen, sample_rate=SYNTH_SAMPLE_RATE)
            if audio.dim() == 1:
                audio = audio.unsqueeze(0)
            audio = audio.squeeze(0).reshape(1, 1, -1).float()
            # Ресемплинг 24000 -> 16000 (модель v5 не даёт 16 кГц напрямую).
            target = max(1, int(round(audio.shape[-1] * SAMPLE_RATE / SYNTH_SAMPLE_RATE)))
            audio = F.interpolate(audio, size=target, mode="linear", align_corners=False)
            samples = audio.reshape(-1).clamp(-1.0, 1.0) * 32767.0
        return samples.to(torch.int16).cpu().numpy().tobytes()

    # ------------------------------------------------------------------ health
    def health(self) -> dict:
        return {
            "provider": self.name,
            "model": self._model_id,
            "speakers": self.speakers(),
            "loaded": self.loaded,
        }
