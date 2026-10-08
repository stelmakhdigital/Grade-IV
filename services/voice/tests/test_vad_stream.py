"""VAD-стрим WS /api/v1/vad/stream (ADR-002, поправка 2026-10-09): state-события.

(1) State-машина StreamVAD: детерминированно на EnergyVAD (синус 300 Гц amp 0.3
    = «речь», нули = тишина): порядок start → pre_end (ровно 1 раз) → end;
    pre_silence не повторяется при продолжении речи.
(2) Реальный Silero VAD (onnx) через endpoint на фикстурах: честная/тихая речь
    детектируется, шум/дыхание — нет (≤ 1 ложное speech-событие).

Стек: реальный uvicorn + websockets (паттерн test_stt_stream.py).
"""
import asyncio
import json
import socket
import threading
import time
from pathlib import Path

import numpy as np
import pytest
import uvicorn
import websockets

from app.main import build_app
from app.providers import SAMPLE_RATE, FakeTTS
from app.stt_stream import EnergyVAD, StreamVAD, VAD_PRE_SILENCE_MS

FIXTURES = Path(__file__).parent / "fixtures"


class NoSTT:
    """STT-заглушка: /vad/stream не распознаёт (build_app требует провайдер)."""

    name = "none"

    def transcribe(self, audio: bytes, sample_rate: int = SAMPLE_RATE):
        from app.providers import STTResult

        return STTResult(text="", confidence=0.0, duration_s=len(audio) / 2 / sample_rate)

    def health(self) -> dict:
        return {"provider": self.name, "model": None, "device": None, "loaded": False}


def _free_port() -> int:
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def _start_uvicorn(vad) -> tuple:
    app = build_app(stt=NoSTT(), tts=FakeTTS(), vad=vad)
    port = _free_port()
    server = uvicorn.Server(uvicorn.Config(app, host="127.0.0.1", port=port, log_level="error"))
    th = threading.Thread(target=server.run, daemon=True)
    th.start()
    deadline = time.time() + 10
    while not server.started and time.time() < deadline:
        time.sleep(0.05)
    assert server.started, "uvicorn не стартовал"
    return server, f"ws://127.0.0.1:{port}/api/v1/vad/stream"


# --- (1) state-машина: детерминированные тесты на EnergyVAD ------------------

# Пороги: конец — тишина 600 мс, pre_end — 400 мс (как в endpoint), речь ≥ 250 мс.
def _engine_vad() -> EnergyVAD:
    return EnergyVAD()


def _tone(ms: int, amp: float = 0.3) -> np.ndarray:
    n = ms * SAMPLE_RATE // 1000
    return (amp * np.sin(2 * np.pi * 300 * np.arange(n) / SAMPLE_RATE)).astype(np.float32)


def test_state_machine_start_pre_end_end() -> None:
    """Речь 1 с + тишина 1.5 с: start → pre_end (1 раз) → end, в этом порядке."""
    v = StreamVAD(_engine_vad().clone(), pre_silence_ms=VAD_PRE_SILENCE_MS)
    audio = np.concatenate([_tone(1000), np.zeros(int(1.5 * SAMPLE_RATE), dtype=np.float32)])
    # Покадрами 250 мс — как реальный поток.
    step = int(0.25 * SAMPLE_RATE)
    events: list[str] = []
    for i in range(0, len(audio), step):
        for kind, _a, _b in v.feed(audio[i:i + step]):
            events.append(kind)
    assert events == ["start", "pre_end", "end"], f"порядок событий: {events}"
    assert events.count("pre_end") == 1, "pre_end пришёл не один раз"


def test_state_machine_pre_end_once_on_resume() -> None:
    """Речь → пауза 500 мс (pre_end, но < 600 мс — реплика не разрывается) → речь
    снова → конец: ровно start → pre_end → end (pre_end — раз за реплику)."""
    v = StreamVAD(_engine_vad().clone(), pre_silence_ms=VAD_PRE_SILENCE_MS)
    audio = np.concatenate([
        _tone(1000), np.zeros(int(0.5 * SAMPLE_RATE), dtype=np.float32),
        _tone(800), np.zeros(int(1.5 * SAMPLE_RATE), dtype=np.float32),
    ])
    events: list[str] = []
    for kind, _a, _b in v.feed(audio):
        events.append(kind)
    assert events == ["start", "pre_end", "end"], f"события: {events}"
    assert events.count("start") == 1  # пауза < min_silence — реплика одна


def test_state_machine_no_pre_end_without_config() -> None:
    """pre_silence_ms=None (дефолт, /stt/stream) — pre_end нет, поведение как было."""
    v = StreamVAD(_engine_vad().clone())
    audio = np.concatenate([_tone(1000), np.zeros(int(1.5 * SAMPLE_RATE), dtype=np.float32)])
    events = [kind for kind, _a, _b in v.feed(audio)]
    assert events == ["start", "end"], f"события: {events}"


def test_state_machine_burst_without_end() -> None:
    """Всплеск < min_speech (250 мс) — не реплика: события end нет."""
    v = StreamVAD(_engine_vad().clone(), pre_silence_ms=VAD_PRE_SILENCE_MS)
    audio = np.concatenate([_tone(150), np.zeros(int(1.5 * SAMPLE_RATE), dtype=np.float32)])
    events = [kind for kind, _a, _b in v.feed(audio)]
    assert "end" not in events, f"всплеск дан как реплика: {events}"


# --- (1b) endpoint: JSON-контракт (EnergyVAD, детерминированно) --------------

@pytest.fixture(scope="module")
def energy_vad_uri():
    server, uri = _start_uvicorn(_engine_vad())
    yield uri
    server.should_exit = True


def _frames(audio: np.ndarray, chunk_ms: int = 250) -> list[bytes]:
    pcm = (np.clip(audio, -1, 1) * 32767).astype(np.int16).tobytes()
    step = chunk_ms * SAMPLE_RATE // 1000 * 2
    return [pcm[i:i + step] for i in range(0, len(pcm), step)] or [b""]


def _run_vad(uri: str, frames: list[bytes], recv_timeout: float, n_states: int):
    """Кадр → кадры, затем n_states state-сообщений (или таймаут)."""
    async def run() -> tuple:
        async with websockets.connect(uri, max_size=None) as ws:
            for f in frames:
                await ws.send(f)
            states: list[dict] = []
            err = None
            while len(states) < n_states:
                try:
                    msg = json.loads(await asyncio.wait_for(ws.recv(), recv_timeout))
                except asyncio.TimeoutError:
                    err = f"timeout: получено {states}"
                    break
                if msg.get("type") == "state":
                    states.append(msg)
            return states, err

    return asyncio.run(run())


async def _recv_timeout(ws, timeout: float):
    return json.loads(await asyncio.wait_for(ws.recv(), timeout))


def test_endpoint_state_sequence_energy(energy_vad_uri: str) -> None:
    """Endpoint: start → pre_silence (ровно 1) → end (JSON-контракт)."""
    audio = np.concatenate([_tone(1000), np.zeros(int(1.5 * SAMPLE_RATE), dtype=np.float32)])
    states, err = _run_vad(energy_vad_uri, _frames(audio), 10, 3)
    assert err is None, f"недополучено событий: {err}"
    assert states[0] == {"type": "state", "speech": True}, states[0]
    assert states[1] == {"type": "state", "speech": True, "pre_silence": True}, states[1]
    assert states[2] == {"type": "state", "speech": False}, states[2]


def test_endpoint_silence_no_events_energy(energy_vad_uri: str) -> None:
    """Endpoint: чистая тишина → ни одного события."""

    async def scenario(uri: str):
        async with websockets.connect(uri, max_size=None) as ws:
            for f in _frames(np.zeros(3 * SAMPLE_RATE, dtype=np.float32)):
                await ws.send(f)
            with pytest.raises(asyncio.TimeoutError):
                await _recv_timeout(ws, 2.0)

    asyncio.run(scenario(energy_vad_uri))


# --- (2) реальный Silero onnx на фикстурах -----------------------------------

def _silero():
    pytest.importorskip("onnxruntime")
    from app.stt_stream import SileroVAD

    try:
        return SileroVAD()
    except Exception as exc:
        pytest.skip(f"Silero VAD (onnx) недоступен: {exc}")


@pytest.fixture(scope="module")
def silero_vad_uri():
    vad = _silero()
    server, uri = _start_uvicorn(vad)
    yield uri
    server.should_exit = True


def _read_fixture(name: str) -> np.ndarray:
    path = FIXTURES / f"{name}.pcm"
    if not path.exists():
        pytest.skip(f"нет фикстуры {path.name} (запустите tests/gen_fixtures.py)")
    return np.frombuffer(path.read_bytes(), dtype=np.int16).astype(np.float32) / 32768


def _silero_fixture_stats(uri: str, name: str) -> dict:
    """Фикстура + 1.5 с тишины → число speech:true / пришёл ли speech:false."""
    tail = np.zeros(int(1.5 * SAMPLE_RATE), dtype=np.float32)
    frames = _frames(np.concatenate([_read_fixture(name), tail]), chunk_ms=250)
    states, _err = _run_vad(uri, frames, 20, 8)
    return {
        "speech_true": sum(1 for s in states if s["speech"]),
        "speech_false": sum(1 for s in states if not s["speech"]),
        "pre_silence": sum(1 for s in states if s.get("pre_silence")),
    }


def test_silero_detects_honest_speech(silero_vad_uri: str) -> None:
    st = _silero_fixture_stats(silero_vad_uri, "speech_honest")
    assert st["speech_true"] >= 1, f"честная речь не детектирована: {st}"
    assert st["speech_false"] >= 1, f"нет окончания реплики: {st}"


def test_silero_detects_quiet_speech(silero_vad_uri: str) -> None:
    st = _silero_fixture_stats(silero_vad_uri, "speech_quiet")
    assert st["speech_true"] >= 1, f"тихая речь не детектирована: {st}"
    assert st["speech_false"] >= 1, f"нет окончания реплики: {st}"


def test_silero_noise_minimal(silero_vad_uri: str) -> None:
    st = _silero_fixture_stats(silero_vad_uri, "noise")
    assert st["speech_true"] <= 1, f"шум дан как речь: {st}"


def test_silero_breathing_minimal(silero_vad_uri: str) -> None:
    st = _silero_fixture_stats(silero_vad_uri, "breathing")
    assert st["speech_true"] <= 1, f"дыхание дано как речь: {st}"
