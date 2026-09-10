"""Абстракции провайдеров STT/TTS (решение #16) и фейки для разработки (WP-1).

Реальные провайдеры подключаются в WP-4:
- STT: faster-whisper (модель из конфига STT_MODEL);
- TTS: Silero v5 (MIT, спикер из конфига TTS_SPEAKER).
Контракты стабильны: /api/v1/stt (аудио-реплика -> текст), /api/v1/tts (текст -> PCM16 16 кГц).
"""
from __future__ import annotations

import math
import struct
from dataclasses import dataclass
from typing import Protocol

SAMPLE_RATE = 16000


@dataclass(frozen=True)
class STTResult:
    """Результат распознавания реплики."""

    text: str
    confidence: float
    duration_s: float


class STTProvider(Protocol):
    """Провайдер STT: аудио-реплика -> текст."""

    name: str

    def transcribe(self, audio: bytes, sample_rate: int = SAMPLE_RATE) -> STTResult:
        """audio — PCM16 mono."""
        ...


class TTSProvider(Protocol):
    """Провайдер TTS: текст -> аудио."""

    name: str

    def synthesize(self, text: str, speaker: str = "ru_01") -> bytes:
        """Возвращает PCM16 mono @ 16 кГц (чистый стриминг по чанкам — WP-4)."""
        ...


class FakeSTT:
    """Dev-провайдер STT: возвращает фиктивный текст и длительность аудио (WP-1)."""

    name = "fake"

    def transcribe(self, audio: bytes, sample_rate: int = SAMPLE_RATE) -> STTResult:
        duration = len(audio) / 2 / sample_rate
        return STTResult(text="[fake stt]", confidence=1.0, duration_s=round(duration, 3))


class FakeTTS:
    """Dev-провайдер TTS: синусоида 440 Гц длительностью ~0.5 с (WP-1)."""

    name = "fake"

    def synthesize(self, text: str, speaker: str = "ru_01") -> bytes:
        n = int(SAMPLE_RATE * 0.5)
        frames = b"".join(
            struct.pack("<h", int(12000 * math.sin(2 * math.pi * 440 * i / SAMPLE_RATE)))
            for i in range(n)
        )
        return frames
