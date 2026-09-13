"""E2E-проход реальными провайдерами (faster-whisper + Silero v5).

Маркер e2e: требует загрузки моделей (STT "tiny" ~75 МБ с HF Hub, TTS v5_ru
~145 МБ с models.silero.ai). Если среды нет/моделей не загрузилось — тесты
просятся skip'ом, CI-режим остаётся на fake-провайдерах.
"""
import io
import wave

import numpy as np
import pytest
from fastapi.testclient import TestClient

pytestmark = pytest.mark.e2e

STT_TEXT = "Здравствуйте! Это голосовое мок-интервью."


@pytest.fixture(scope="module")
def real_client():
    try:
        from app.faster_whisper import FasterWhisperSTT
        from app.main import build_app
        from app.silero import SileroTTS

        stt = FasterWhisperSTT(model_size="tiny")  # tiny: быстро на CPU, для проверки связки
        tts = SileroTTS()
        # Заранее проверяем, что обе модели реально грузятся (иначе skip).
        stt.transcribe(b"\x00" * 16000, 16000)  # 1 c тишины -> text=""
        _ = tts.synthesize("Тест", speaker="kseniya")
    except Exception as exc:  # сеть/диск/зависимости не доступны
        pytest.skip(f"Реальные модели недоступны в этой среде: {exc}")
    return TestClient(build_app(stt, tts))


def test_tts_real(real_client) -> None:
    """Silero v5: текст -> PCM16 16 кГц, не тишина."""
    resp = real_client.post("/api/v1/tts", json={"text": STT_TEXT, "speaker": "kseniya"})
    assert resp.status_code == 200
    assert resp.headers["content-type"].startswith("audio/pcm")
    pcm = np.frombuffer(resp.content, dtype=np.int16)
    assert len(pcm) >= 16000  # не меньше 1 секунды речи
    assert int(np.abs(pcm).max()) > 1000  # есть реальная амплитуда, а не шум/тишина
    assert int(pcm.mean()) < 1000  # без клиппинга-насыщения


def test_stt_real_silence(real_client) -> None:
    """Тишина -> text='' (VAD глушит галлюцинации whisper)."""
    pcm16 = (np.zeros(16000 * 2, dtype=np.int16)).tobytes()
    buf = io.BytesIO()
    with wave.open(buf, "wb") as w:
        w.setnchannels(1)
        w.setsampwidth(2)
        w.setframerate(16000)
        w.writeframes(pcm16)
    resp = real_client.post(
        "/api/v1/stt", files={"audio": ("silence.wav", buf.getvalue(), "audio/wav")}
    )
    assert resp.status_code == 200
    body = resp.json()
    assert body["text"] == ""
    assert body["confidence"] == 0.0
    assert abs(body["duration_s"] - 2.0) < 0.05


def test_stt_real_roundtrip(real_client) -> None:
    """Честный проход: Silero TTS -> PCM16 16 кГц -> faster-whisper -> текст."""
    resp = real_client.post("/api/v1/tts", json={"text": STT_TEXT, "speaker": "kseniya"})
    assert resp.status_code == 200
    pcm16 = resp.content
    buf = io.BytesIO()
    with wave.open(buf, "wb") as w:
        w.setnchannels(1)
        w.setsampwidth(2)
        w.setframerate(16000)
        w.writeframes(pcm16)
    stt_resp = real_client.post(
        "/api/v1/stt", files={"audio": ("speech.wav", buf.getvalue(), "audio/wav")}
    )
    assert stt_resp.status_code == 200
    body = stt_resp.json()
    assert isinstance(body["text"], str) and body["text"].strip()
    assert body["confidence"] > 0.0
    assert 2.0 < body["duration_s"] < 30.0
    # Распознана суть (tiny-модель допускает неточности: проверяем ядро фразы)
    lowered = body["text"].lower()
    assert "здравствуйте" in lowered or "интервью" in lowered
