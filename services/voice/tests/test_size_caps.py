"""Потолки размера входа (аудит 2026-11-26): сервис не должен съедать память
или воркер TTS по одному чужому запросу — у voice нет ни аутентификации, ни
лимитов, и до этого правки порту наружуpublish не было.
"""
from fastapi.testclient import TestClient

from app.main import MAX_AUDIO_BYTES, MAX_TTS_CHARS, build_app
from app.providers import FakeSTT, FakeTTS
from app.stt_stream import EnergyVAD

client = TestClient(build_app(FakeSTT(), FakeTTS(), EnergyVAD()))


def test_stt_rejects_oversized_audio() -> None:
    audio = b"\x00\x01" * (MAX_AUDIO_BYTES // 2 + 8)
    assert len(audio) > MAX_AUDIO_BYTES
    resp = client.post(
        "/api/v1/stt", files={"audio": ("big.pcm", audio, "application/octet-stream")}
    )
    assert resp.status_code == 413, resp.text
    assert resp.json()["code"] == "audio_too_large"


def test_stt_accepts_audio_at_the_limit() -> None:
    # Граница включительно: потолок не должен отрезать законную реплику.
    audio = b"\x00\x01" * (MAX_AUDIO_BYTES // 2)
    assert len(audio) == MAX_AUDIO_BYTES
    resp = client.post(
        "/api/v1/stt", files={"audio": ("ok.pcm", audio, "application/octet-stream")}
    )
    assert resp.status_code == 200, resp.text
    assert resp.json()["text"] == "[fake stt]"


def test_tts_rejects_overlong_text() -> None:
    resp = client.post("/api/v1/tts", json={"text": "а" * (MAX_TTS_CHARS + 1)})
    assert resp.status_code == 422  # pydantic: поле не прошло валидацию
    assert b"too_long" in resp.content


def test_tts_accepts_text_at_the_limit() -> None:
    resp = client.post("/api/v1/tts", json={"text": "а" * MAX_TTS_CHARS})
    assert resp.status_code == 200, resp.text
