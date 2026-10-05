"""Стриминговый STT WS /api/v1/stt/stream (ADR-007): partial/final/state.

Стек: реальный uvicorn (тестовый порт) + websockets-клиент с таймаутами.
VAD — энергетический (детерминированный, onnx не нужен); STT — LengthSTT:
текст зависит от длительности и уровня аудио (хвост реплики виден в финале).
Синтетическая «речь» — синус 300 Гц (amp 0.3), тишина — нули.
"""
import asyncio
import json
import socket
import threading
import time

import numpy as np
import pytest
import uvicorn
import websockets

from app.main import build_app
from app.providers import SAMPLE_RATE, FakeTTS, STTResult
from app.stt_stream import EnergyVAD, build_vad


class LengthSTT:
    """Fake-STT: текст кодирует длительность и уровень аудио (для проверок хвоста)."""

    name = "length-fake"

    def transcribe(self, audio: bytes, sample_rate: int = SAMPLE_RATE) -> STTResult:
        x = np.frombuffer(audio, dtype=np.int16).astype(np.float32) / 32768
        rms = float(np.sqrt(np.mean(x**2))) if x.size else 0.0
        if rms < 0.005:
            return STTResult(text="", confidence=0.0, duration_s=len(x) / sample_rate)
        dur = len(x) / sample_rate
        return STTResult(text=f"[fake {dur:.1f}s]", confidence=0.9, duration_s=round(dur, 3))

    def health(self) -> dict:
        return {"provider": self.name, "model": None, "device": None, "loaded": False}


def _free_port() -> int:
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


@pytest.fixture(scope="module")
def voice_uri():
    """Один uvicorn на модуль: EnergyVAD (быстрые пороги) + LengthSTT."""
    vad = EnergyVAD(min_silence_ms=150, min_speech_ms=100)
    app = build_app(stt=LengthSTT(), tts=FakeTTS(), vad=vad)
    port = _free_port()
    server = uvicorn.Server(uvicorn.Config(app, host="127.0.0.1", port=port, log_level="error"))
    th = threading.Thread(target=server.run, daemon=True)
    th.start()
    deadline = time.time() + 10
    while not server.started and time.time() < deadline:
        time.sleep(0.05)
    assert server.started, "uvicorn не стартовал"
    yield f"ws://127.0.0.1:{port}/api/v1/stt/stream"
    server.should_exit = True
    th.join(timeout=5)


def _tone(ms: int, amp: float = 0.3) -> np.ndarray:
    n = ms * SAMPLE_RATE // 1000
    return (amp * np.sin(2 * np.pi * 300 * np.arange(n) / SAMPLE_RATE)).astype(np.float32)


def _frames(audio: np.ndarray, chunk_ms: int = 250) -> list[bytes]:
    pcm = (audio * 32767).astype(np.int16).tobytes()
    step = chunk_ms * SAMPLE_RATE // 1000 * 2
    return [pcm[i:i + step] for i in range(0, len(pcm), step)] or [b""]


async def _recv(ws, timeout: float) -> dict:
    return json.loads(await asyncio.wait_for(ws.recv(), timeout))


async def _send_frames(ws, frames: list[bytes]) -> None:
    for f in frames:
        await ws.send(f)


async def _run(voice_uri: str, frames: list[bytes], timeout: float):
    """Отправка кадров → сообщения до final (или таймаут) → (msgs, err)."""
    async with websockets.connect(voice_uri, max_size=None) as ws:
        await _send_frames(ws, frames)
        msgs: list[dict] = []
        err = None
        while True:
            try:
                msgs.append(await _recv(ws, timeout))
            except asyncio.TimeoutError:
                err = "timeout"
                break
            if msgs[-1]["type"] == "final":
                break
        return msgs, err


def test_partial_arrives_before_final(voice_uri: str) -> None:
    """(1) partial приходит до final (речь 3 с → partial на буфере «как есть»)."""
    async def scenario():
        return await _run(voice_uri, _frames(_tone(3000)) + _frames(np.zeros(12800)), 10)

    msgs, err = asyncio.run(scenario())
    assert err is None, f"final не пришёл: {msgs}"
    types = [m["type"] for m in msgs]
    assert "state" in types and types[0] == "state"
    assert "partial" in types, f"нет partial: {types}"
    assert types.index("partial") < types.index("final"), f"порядок: {types}"
    partials = [m for m in msgs if m["type"] == "partial"]
    assert all(m.get("text") for m in partials), "partial с пустым текстом"


def test_final_contains_full_utterance(voice_uri: str) -> None:
    """(2) final содержит всю реплику: длительность финала ≈ 3 с (хвост не потерян)."""
    async def scenario():
        return await _run(voice_uri, _frames(_tone(3000)) + _frames(np.zeros(12800)), 10)

    msgs, err = asyncio.run(scenario())
    assert err is None, f"final не пришёл: {msgs}"
    final = msgs[-1]
    assert final["type"] == "final"
    # LengthSTT кодирует длительность: «[fake X.Xs]» — X ≈ 3.0–3.5 (pre-roll/хвост).
    seconds = float(final["text"].removeprefix("[fake ").removesuffix("s]"))
    assert seconds >= 2.9, f"хвост реплики потерян (final на {seconds:.1f} с из 3 с): {final}"
    assert final["confidence"] > 0
    # state-пара: speech true ... speech false
    states = [m["speech"] for m in msgs if m["type"] == "state"]
    assert states[0] is True and states[-1] is False


def test_silence_no_partial_no_final(voice_uri: str) -> None:
    """(3) тишина без речи → ни partial, ни final (ни одного сообщения)."""

    async def scenario():
        async with websockets.connect(voice_uri, max_size=None) as ws:
            await _send_frames(ws, _frames(np.zeros(3 * SAMPLE_RATE)))
            with pytest.raises(asyncio.TimeoutError):
                await _recv(ws, 2.0)

    asyncio.run(scenario())


def test_health_reports_vad_name() -> None:
    from fastapi.testclient import TestClient

    app = build_app(stt=LengthSTT(), tts=FakeTTS(), vad=EnergyVAD())
    body = TestClient(app).get("/api/v1/health").json()
    assert body["vad"] == "energy"


def test_default_vad_is_silero() -> None:
    """Prod-дефолт: Silero VAD (onnx bundled в faster-whisper)."""
    assert build_vad().name == "silero"
