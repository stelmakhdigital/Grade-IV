"""grade-voice: FastAPI-сервис STT/TTS (WP-1: health + фейки; WP-4: faster-whisper/Silero)."""
from fastapi import FastAPI

from .providers import FakeSTT, FakeTTS

app = FastAPI(title="Grade Voice", version="0.1.0")

stt = FakeSTT()
tts = FakeTTS()


@app.get("/api/v1/health")
def health() -> dict:
    """Состояние сервиса и провайдеров (используется health-check из api)."""
    return {
        "service": "grade-voice",
        "stt": {"provider": stt.name},
        "tts": {"provider": tts.name, "speakers": ["ru_01"]},
    }


# WP-4: POST /api/v1/stt (multipart audio -> {text, confidence, duration_s}),
# POST /api/v1/tts ({"text", "speaker"} -> audio/pcm, chunked-стрим).
