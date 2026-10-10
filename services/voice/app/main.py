"""grade-voice: FastAPI-сервис STT/TTS (WP-1: health + фейки; WP-4: faster-whisper/Silero).

Контракт — ARCHITECTURE.md §4.3:
- POST /api/v1/stt — multipart: ``audio`` (WAV/PCM16 mono, 16 кГц), опц. ``sample_rate``
  -> ``{"text", "confidence", "duration_s"}``; ошибка/молчание -> ``text=""`` (не 500).
- POST /api/v1/tts — JSON ``{"text", "speaker?"}`` -> 200 ``audio/pcm``
  (PCM16 mono 16 кГц, без WAV-заголовка, стрим по чанкам ~250 мс);
  пустой текст -> 400 ``{"code", "message"}``.
- GET /api/v1/health — состояние сервиса и провайдеров (health-check из api).

Выбор провайдеров — env (режим CI/mock: "fake"):
- ``VOICE_STT_PROVIDER``: faster-whisper (default) | fake
- ``VOICE_TTS_PROVIDER``: silero (default) | fake
"""
from __future__ import annotations

import logging
import os

from fastapi import FastAPI, File, Form, UploadFile
from fastapi.responses import JSONResponse, StreamingResponse
from pydantic import BaseModel

from .providers import SAMPLE_RATE, FakeSTT, FakeTTS
from .stt_stream import build_vad, register_stt_stream, register_vad_stream

logger = logging.getLogger("voice")

# Пачка стриминга TTS: ~250 мс @ 16 кГц PCM16 mono.
CHUNK_BYTES = int(SAMPLE_RATE * 2 * 0.25)


def _build_stt():
    """STT-провайдер по env VOICE_STT_PROVIDER (default: faster-whisper)."""
    name = os.getenv("VOICE_STT_PROVIDER", "faster-whisper").strip().lower()
    if name == "fake":
        return FakeSTT()
    if name in ("", "faster-whisper"):
        from .faster_whisper import FasterWhisperSTT

        return FasterWhisperSTT(
            model_size=os.getenv("STT_MODEL", "small"),
            language=os.getenv("STT_LANGUAGE", "ru"),
        )
    raise ValueError(f"Неизвестный VOICE_STT_PROVIDER: {name!r}")


def _build_tts():
    """TTS-провайдер по env VOICE_TTS_PROVIDER (default: silero)."""
    name = os.getenv("VOICE_TTS_PROVIDER", "silero").strip().lower()
    if name == "fake":
        return FakeTTS()
    if name in ("", "silero"):
        from .silero import SileroTTS

        return SileroTTS(default_speaker=os.getenv("TTS_SPEAKER", "ru_01"))
    raise ValueError(f"Неизвестный VOICE_TTS_PROVIDER: {name!r}")


def build_app(stt=None, tts=None, vad=None, max_speech_s: float | None = None) -> FastAPI:
    """Собирает FastAPI-приложение с заданными (или env-выбранными) провайдерами.

    Тесты передают провайдеры явно (fake/реальные), prod — `app` ниже.
    ``max_speech_s`` — лимит непрерывной речи в реплике (None → VAD_MAX_SPEECH_S из env/константы).
    """
    app = FastAPI(title="Grade Voice", version="0.2.0")

    stt = stt if stt is not None else _build_stt()
    tts = tts if tts is not None else _build_tts()
    vad = vad if vad is not None else build_vad()
    app.state.stt = stt
    app.state.tts = tts
    app.state.vad = vad

    @app.get("/api/v1/health")
    def health() -> dict:
        """Состояние сервиса и провайдеров (используется health-check из api)."""
        return {"service": "grade-voice", "stt": stt.health(), "tts": tts.health(), "vad": vad.name}

    # Стриминговый STT (ADR-007): WS /api/v1/stt/stream — PCM-кадры →
    # state/partial/final (Silero VAD, partial каждые ~500 мс).
    register_stt_stream(app, stt, vad, max_speech_s=max_speech_s)
    # VAD-стрим без STT (ADR-002, поправка 2026-10-09): WS /api/v1/vad/stream —
    # state-события Silero VAD для batch-пути (pre-STT + batch /stt в api).
    register_vad_stream(app, vad)

    @app.post("/api/v1/stt")
    async def stt_endpoint(
        audio: UploadFile = File(...),
        sample_rate: int = Form(SAMPLE_RATE),
    ) -> dict:
        """Аудио-реплика (WAV/PCM16 mono) -> {text, confidence, duration_s}."""
        data = await audio.read()
        result = stt.transcribe(data or b"", sample_rate or SAMPLE_RATE)
        return {
            "text": result.text,
            "confidence": result.confidence,
            "duration_s": result.duration_s,
        }

    @app.post("/api/v1/tts")
    def tts_endpoint(payload: TTSRequest):
        """Текст -> стрим PCM16 mono 16 кГц (audio/pcm, чанки ~250 мс)."""
        text = (payload.text or "").strip()
        if not text:
            return JSONResponse(
                status_code=400,
                content={"code": "empty_text", "message": "Поле 'text' обязательно и не может быть пустым"},
            )
        try:
            pcm = tts.synthesize(text, speaker=payload.speaker or os.getenv("TTS_SPEAKER", "ru_01"))
        except Exception as exc:  # ошибка модели/сети — 500 с кодом (STT-договор "не 500" на STT)
            logger.exception("Ошибка синтеза речи")
            return JSONResponse(
                status_code=500,
                content={"code": "tts_error", "message": str(exc)},
            )
        return StreamingResponse(
            _chunks(pcm),
            media_type="audio/pcm",
            headers={"X-Sample-Rate": str(SAMPLE_RATE), "X-Channels": "1", "X-Bits": "16"},
        )

    return app


def _chunks(pcm: bytes):
    """Режет PCM на чанки ~250 мс для стриминга."""
    for i in range(0, len(pcm), CHUNK_BYTES):
        yield pcm[i : i + CHUNK_BYTES]


class TTSRequest(BaseModel):
    text: str = ""
    speaker: str | None = None


#: Приложение для uvicorn (make run-voice): провайдеры выбираются из env.
app = build_app()
