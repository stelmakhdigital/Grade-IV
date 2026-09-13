"""HTTP-контракт /api/v1/stt и /api/v1/tts на фейковых провайдерах (WP-4)."""
import io
import math
import struct
import wave

from fastapi.testclient import TestClient

from app.main import build_app
from app.providers import FakeSTT, FakeTTS, SAMPLE_RATE

client = TestClient(build_app(FakeSTT(), FakeTTS()))


def _pcm16(freq: float = 440.0, seconds: float = 0.5, sr: int = SAMPLE_RATE) -> bytes:
    return b"".join(
        struct.pack("<h", int(12000 * math.sin(2 * math.pi * freq * i / sr)))
        for i in range(int(sr * seconds))
    )


def _wav(pcm16: bytes, sr: int = SAMPLE_RATE) -> bytes:
    buf = io.BytesIO()
    with wave.open(buf, "wb") as w:
        w.setnchannels(1)
        w.setsampwidth(2)
        w.setframerate(sr)
        w.writeframes(pcm16)
    return buf.getvalue()


# ------------------------------------------------------------------ STT
def test_stt_raw_pcm() -> None:
    resp = client.post(
        "/api/v1/stt",
        files={"audio": ("a.pcm", _pcm16(), "application/octet-stream")},
        data={"sample_rate": "16000"},
    )
    assert resp.status_code == 200
    body = resp.json()
    assert set(body) == {"text", "confidence", "duration_s"}
    assert body["text"] == "[fake stt]"
    assert body["confidence"] == 1.0
    assert abs(body["duration_s"] - 0.5) < 0.01


def test_stt_wav_container() -> None:
    resp = client.post(
        "/api/v1/stt",
        files={"audio": ("a.wav", _wav(_pcm16(seconds=1.0)), "audio/wav")},
    )
    assert resp.status_code == 200
    body = resp.json()
    assert body["text"] == "[fake stt]"
    assert abs(body["duration_s"] - 1.0) < 0.01


def test_stt_empty_audio_returns_empty_text() -> None:
    resp = client.post(
        "/api/v1/stt",
        files={"audio": ("empty.pcm", b"", "application/octet-stream")},
    )
    assert resp.status_code == 200  # ошибка/пусто — не 500
    body = resp.json()
    assert body["text"] == ""
    assert body["confidence"] == 0.0
    assert body["duration_s"] == 0.0


# ------------------------------------------------------------------ TTS
def test_tts_ok_pcm_stream() -> None:
    resp = client.post("/api/v1/tts", json={"text": "Здравствуйте!"})
    assert resp.status_code == 200
    assert resp.headers["content-type"].startswith("audio/pcm")
    pcm = resp.content
    # FakeTTS: 0.5 c @ 16 кГц PCM16 mono
    assert len(pcm) == int(SAMPLE_RATE * 0.5 * 2)
    assert pcm != b"\x00" * len(pcm)  # не тишина


def test_tts_with_speaker() -> None:
    resp = client.post("/api/v1/tts", json={"text": "Текст", "speaker": "ru_01"})
    assert resp.status_code == 200
    assert resp.headers["content-type"].startswith("audio/pcm")


def test_tts_empty_text_400() -> None:
    for payload in ({}, {"text": ""}, {"text": "   "}):
        resp = client.post("/api/v1/tts", json=payload)
        assert resp.status_code == 400
        body = resp.json()
        assert body["code"] == "empty_text"
        assert "message" in body
