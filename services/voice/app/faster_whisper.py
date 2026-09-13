"""STT-провайдер: faster-whisper (self-hosted, CPU/GPU; решение #16, ARCH §4.3).

Модель (tiny/base/small/mid/large-v3) — из env ``STT_MODEL`` (default: "small").
LAZY LOADING: модель грузится при первом запросе, а не при старте сервиса
(health не требует модели; старт остаётся быстрым). Повторные загрузки не
дублируются: singleton + lock.

Вход — PCM16 mono: raw-байты или WAV-контейнер (определяется по заголовку RIFF).
Ошибка/неподдерживаемый формат/молчание -> ``STTResult(text="", ...)`` (не 500).
"""
from __future__ import annotations

import logging
import os
import struct
import threading

import numpy as np

from .providers import SAMPLE_RATE, STTResult

logger = logging.getLogger("voice.stt")

MODEL_SIZES = ("tiny", "base", "small", "mid", "medium", "large-v3")


def extract_pcm16(data: bytes) -> tuple[bytes, int] | None:
    """Извлекает PCM16 mono из буфера: WAV (RIFF) или raw PCM16.

    Возвращает ``(pcm, sample_rate)`` (sample_rate=0 — raw, задаёт вызывающий)
    или ``None``, если формат не поддерживается (не PCM16 mono / битый заголовок).
    """
    if data[:4] == b"RIFF" and data[8:12] == b"WAVE":
        if len(data) < 44:
            return None
        audio_format = struct.unpack_from("<H", data, 20)[0]
        channels = struct.unpack_from("<H", data, 22)[0]
        sample_rate = struct.unpack_from("<I", data, 24)[0]
        bits = struct.unpack_from("<H", data, 34)[0]
        if audio_format != 1 or bits != 16 or channels != 1:
            return None
        pos = 12
        while pos + 8 <= len(data):
            chunk_id = data[pos : pos + 4]
            chunk_size = struct.unpack_from("<I", data, pos + 4)[0]
            if chunk_id == b"data":
                return data[pos + 8 : pos + 8 + chunk_size], sample_rate
            pos += 8 + chunk_size + (chunk_size % 2)  # чанки выравниваются по 2 байта
        return None
    return data, 0  # raw PCM16 mono


class FasterWhisperSTT:
    """faster-whisper (ctranslate2) — STT-провайдер с ленивой загрузкой модели."""

    name = "faster-whisper"

    def __init__(
        self,
        model_size: str = "small",
        language: str = "ru",
        device: str = "",
        compute_type: str = "",
        no_speech_prob: float = 0.6,
        beam_size: int = 5,
    ) -> None:
        self._size = model_size if model_size in MODEL_SIZES else "small"
        if model_size not in MODEL_SIZES:
            logger.warning("STT_MODEL=%r неизвестен; использован default 'small'", model_size)
        self._language = language
        self._no_speech_prob = no_speech_prob
        self._beam_size = beam_size
        # Device: env STT_DEVICE (cpu | cuda); default cpu — dev-среда без GPU.
        # (faster-whisper 1.x не предоставляет авто-детект device публичным API.)
        self._device = (device or os.getenv("STT_DEVICE", "cpu")).lower()
        self._compute_type = compute_type or os.getenv(
            "STT_COMPUTE_TYPE", "int8" if self._device == "cpu" else "float16"
        )
        self._model = None
        self._lock = threading.Lock()

    # ------------------------------------------------------------------ lazy
    def _ensure_model(self):
        """Загружает модель один раз (singleton + lock); блокирует только на загрузке."""
        if self._model is None:
            with self._lock:
                if self._model is None:
                    from faster_whisper import WhisperModel

                    logger.info(
                        "Загрузка STT-модели faster-whisper=%s device=%s compute=%s",
                        self._size,
                        self._device,
                        self._compute_type,
                    )
                    self._model = WhisperModel(
                        self._size, device=self._device, compute_type=self._compute_type
                    )
        return self._model

    @property
    def loaded(self) -> bool:
        return self._model is not None

    # --------------------------------------------------------------- contract
    def transcribe(self, audio: bytes, sample_rate: int = SAMPLE_RATE) -> STTResult:
        """PCM16 mono (raw или WAV) -> текст. Ошибки/молчание -> text='' (не 500)."""
        sr = sample_rate or SAMPLE_RATE
        duration = (len(audio) / 2 / sr) if audio else 0.0
        try:
            extracted = extract_pcm16(audio)
            if extracted is None or not extracted[0]:
                logger.warning("Пустое/неподдерживаемое аудио (%d байт) -> text=''", len(audio))
                return STTResult(text="", confidence=0.0, duration_s=round(duration, 3))
            pcm, wav_sr = extracted
            if len(pcm) % 2:
                pcm = pcm[:-1]
            sr_eff = wav_sr or sr
            samples = np.frombuffer(pcm, dtype=np.int16).astype(np.float32) / 32768.0
            if sr_eff != SAMPLE_RATE and len(samples) > 1:
                n = max(2, int(round(len(samples) * SAMPLE_RATE / sr_eff)))
                samples = np.interp(
                    np.linspace(0.0, len(samples) - 1, n), np.arange(len(samples)), samples
                )
                duration = n / SAMPLE_RATE
            model = self._ensure_model()
            segments, _info = model.transcribe(
                samples,
                language=self._language,
                beam_size=self._beam_size,
                vad_filter=True,  # Silero VAD (onnx, в комплекте с faster-whisper): глушит галлюцинации на тишине
            )
            kept = [s for s in segments if s.no_speech_prob < self._no_speech_prob]
            if not kept:
                return STTResult(text="", confidence=0.0, duration_s=round(duration, 3))
            text = " ".join(s.text.strip() for s in kept).strip()
            # Аппроксимация уверенности: среднее экспонент avg_logprob (logprob <= 0).
            confidence = float(
                np.clip(np.mean([float(np.exp(s.avg_logprob)) for s in kept]), 0.0, 1.0)
            )
            return STTResult(text=text, confidence=round(confidence, 3), duration_s=round(duration, 3))
        except Exception:
            logger.exception("STT-распознавание завершилось ошибкой -> text=''")
            return STTResult(text="", confidence=0.0, duration_s=round(duration, 3))

    # ------------------------------------------------------------------ health
    def health(self) -> dict:
        return {
            "provider": self.name,
            "model": self._size,
            "device": self._device,
            "loaded": self.loaded,
        }
