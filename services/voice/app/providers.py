"""Абстракции провайдеров STT/TTS (решение #16) и фейки для разработки (WP-1).

Реальные провайдеры подключаются в WP-4:
- STT: faster-whisper (модель из конфига STT_MODEL);
- TTS: Silero v5 (MIT, спикер из конфига TTS_SPEAKER).
Контракты стабильны: /api/v1/stt (аудио-реплика -> текст), /api/v1/tts (текст -> PCM16 16 кГц).
"""
from __future__ import annotations

import concurrent.futures
import math
import struct
from dataclasses import dataclass
from typing import Protocol

SAMPLE_RATE = 16000

# ctranslate2 (faster-whisper) и torch (GigaAM) не допускают параллельного
# transcribe одной модели из разных потоков (гонка partial+final → RuntimeError
# в model.encode, сегмент терялся — BUG-20261012-018): все STT-вызовы процесса
# идут через ОДНОПОТОЧНЫЙ исполнитель (сериализация; GPU-запросы по мс — очередь
# не ощутима, на CPU это защита от крашей).
STT_EXECUTOR = concurrent.futures.ThreadPoolExecutor(max_workers=1, thread_name_prefix="stt")


def extract_pcm16(data: bytes) -> tuple[bytes, int] | None:
    """Извлекает PCM16 mono из буфера: WAV (RIFF) или raw PCM16.

    Возвращает ``(pcm, sample_rate)`` (sample_rate=0 — raw, задаёт вызывающий)
    или ``None``, если формат не поддерживается (не PCM16 mono / битый заголовок).
    Общий утил리티 для всех STT-провайдеров (faster-whisper, gigaam).
    """
    import struct

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
        """audio — PCM16 mono (raw или WAV)."""
        ...

    def health(self) -> dict:
        """Состояние провайдера для GET /api/v1/health: {"provider","model","device","loaded"}."""
        ...


class TTSProvider(Protocol):
    """Провайдер TTS: текст -> аудио."""

    name: str

    def synthesize(self, text: str, speaker: str = "ru_01") -> bytes:
        """Возвращает PCM16 mono @ 16 кГц (чистый стриминг по чанкам — WP-4)."""
        ...

    def health(self) -> dict:
        """Состояние провайдера для GET /api/v1/health: {"provider","speakers":[],"loaded"}."""
        ...


class FakeSTT:
    """Dev-провайдер STT: возвращает фиктивный текст и длительность аудио (WP-1)."""

    name = "fake"

    def transcribe(self, audio: bytes, sample_rate: int = SAMPLE_RATE) -> STTResult:
        if not audio:
            return STTResult(text="", confidence=0.0, duration_s=0.0)
        duration = len(audio) / 2 / sample_rate
        return STTResult(text="[fake stt]", confidence=1.0, duration_s=round(duration, 3))

    def health(self) -> dict:
        return {"provider": self.name, "model": None, "device": None, "loaded": False}


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

    def health(self) -> dict:
        return {"provider": self.name, "model": None, "speakers": ["ru_01"], "loaded": False}
