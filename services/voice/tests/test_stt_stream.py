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


def test_partial_carries_speech_ms(voice_uri: str) -> None:
    """(1b) partial несёт speech_ms — сколько речи в сегменте НА МОМЕНТ СНИМКА.

    Без этого клиент узнавал длительность речи только на границе тишины
    (stt_segment), и счётчик «речи N с» в окне записи стоял на нуле всю
    реплику, пока кандидат говорит (ui-review 2026-11-28).
    """
    async def scenario():
        return await _run(voice_uri, _frames(_tone(3000)) + _frames(np.zeros(12800)), 10)

    msgs, err = asyncio.run(scenario())
    assert err is None, f"final не пришёл: {msgs}"
    partials = [m for m in msgs if m["type"] == "partial"]
    assert partials, "нет partial"
    ms = [m.get("speech_ms", 0) for m in partials]
    assert all(v > 0 for v in ms), f"partial без speech_ms: {ms}"
    assert ms == sorted(ms), f"speech_ms не растёт вместе с буфером: {ms}"
    # Снимок не может быть длиннее самой речи (+ pre-roll/хвост кадра).
    assert ms[-1] <= 3600, f"speech_ms больше речи: {ms[-1]}"


class SlowLengthSTT(LengthSTT):
    """LengthSTT с задержкой распознавалки: питает adaptive-throttle partial'ов."""

    def __init__(self, delay: float) -> None:
        self.delay = delay

    def transcribe(self, audio: bytes, sample_rate: int = SAMPLE_RATE) -> STTResult:
        time.sleep(self.delay)
        return super().transcribe(audio, sample_rate)


@pytest.fixture(scope="module")
def slow_voice_uri():
    """Uvicorn с медленным STT (0.6 с на распознавалку) — как GigaAM на CPU."""
    vad = EnergyVAD(min_silence_ms=150, min_speech_ms=100)
    app = build_app(stt=SlowLengthSTT(0.6), tts=FakeTTS(), vad=vad)
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


def test_partial_throttles_to_its_own_cost(slow_voice_uri: str) -> None:
    """(1c) adaptive-throttle: partial не чаще, чем 2× его собственной стоимости.

    Речь 4 с кадрми по 250 мс в реальном времени, распознавалка 0.6 с. С
    фиксированным интервалом 0.5 с partial'ы шли бы каждые ~0,6 с (6-7 штук) и
    держали бы однопоточный STT_EXECUTOR занятым — final ждал бы своей очереди
    (замер в проде: 2-3,6 с до счётчика речи). С throttle интервал 1,2 с.
    """
    async def scenario():
        async with websockets.connect(slow_voice_uri, max_size=None) as ws:
            got: list[tuple[float, dict]] = []
            frames = _frames(_tone(4000)) + _frames(np.zeros(12800))
            stop = asyncio.Event()

            async def reader():
                while not stop.is_set():
                    try:
                        m = await _recv(ws, 0.2)
                    except asyncio.TimeoutError:
                        continue
                    except websockets.exceptions.ConnectionClosed:
                        break
                    got.append((time.monotonic(), m))
                    if m["type"] == "final":
                        return

            r = asyncio.create_task(reader())
            for f in frames:
                await ws.send(f)
                await asyncio.sleep(0.25)
            await asyncio.wait_for(r, 10)
            stop.set()
            r.cancel()
            return got

    got = asyncio.run(scenario())
    part_t = [t for t, m in got if m["type"] == "partial"]
    assert len(part_t) >= 2, f"мало partial'ов для замера интервала: {part_t}"
    gaps = [b - a for a, b in zip(part_t, part_t[1:])]
    assert min(gaps) >= 1.0, f"partial чаще, чем 2× стоимости (0.6 с): {[f'{g:.2f}' for g in gaps]}"
    # Живой текст при этом остаётся: final догнал речь.
    final = [m for _, m in got if m["type"] == "final"]
    assert final and final[0]["text"], f"final без текста: {final}"


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


def _run_collect_all(voice_uri: str, frames: list[bytes], timeout: float, stop_states: int = 1):
    """Отправка кадров → сборка сообщений до финального state(speech=False) или таймаута.

    В отличие от ``_run``, собирает ВСЕ finals (для проверки принудительного разрыва).
    """
    async def scenario():
        async with websockets.connect(voice_uri, max_size=None) as ws:
            await _send_frames(ws, frames)
            msgs: list[dict] = []
            err = None
            end_states = 0
            while True:
                try:
                    m = await _recv(ws, timeout)
                except asyncio.TimeoutError:
                    err = "timeout"
                    break
                msgs.append(m)
                if m["type"] == "state" and m.get("speech") is False:
                    end_states += 1
                # после естественного конца (state False) идёт хвостовой final —
                # ждём его и завершаем (split-final'ы приходят только во время речи).
                if end_states >= stop_states and m["type"] == "final":
                    break
            return msgs, err

    return asyncio.run(scenario())


def test_max_speech_forced_split() -> None:
    """Лимит непрерывной речи: речь 4 с при max_speech_s=1.5 → ≥2 final (разрыв)."""
    vad = EnergyVAD(min_silence_ms=150, min_speech_ms=100)
    app = build_app(stt=LengthSTT(), tts=FakeTTS(), vad=vad, max_speech_s=1.5)
    port = _free_port()
    server = uvicorn.Server(uvicorn.Config(app, host="127.0.0.1", port=port, log_level="error"))
    th = threading.Thread(target=server.run, daemon=True)
    th.start()
    deadline = time.time() + 10
    while not server.started and time.time() < deadline:
        time.sleep(0.05)
    assert server.started, "uvicorn не стартовал"
    try:
        uri = f"ws://127.0.0.1:{port}/api/v1/stt/stream"
        # 4 с непрерывной речи + тишина 1 с (16000 сэмплов, ≥ min_silence) для естественного конца.
        frames = _frames(_tone(4000)) + _frames(np.zeros(16000))
        msgs, err = _run_collect_all(uri, frames, timeout=10)
        assert err is None, f"не дождались конца: {msgs}"
        finals = [m for m in msgs if m["type"] == "final"]
        # 4 с / 1.5 с → минимум 2 сегмента; хвост + разрывы.
        assert len(finals) >= 2, f"ожидал ≥2 final (разрыв), получилось {len(finals)}: {msgs}"
        # первый финал — принудительный (split=True), его длительность ≈ ≤ 1.5–2 с.
        assert finals[0].get("split") is True, f"первый final не помечен split: {finals[0]}"
        first_s = float(finals[0]["text"].removeprefix("[fake ").removesuffix("s]"))
        assert first_s <= 2.0, f"первый сегмент длиннее лимита: {first_s:.1f} с"
        # state-пара: первый state — speech true, последний — speech false.
        states = [m["speech"] for m in msgs if m["type"] == "state"]
        assert states[0] is True and states[-1] is False
        # суммарно все сегменты покрывают ~4 с речи (с учётом pre-roll/хвоста).
        total_s = sum(float(f["text"].removeprefix("[fake ").removesuffix("s]")) for f in finals)
        assert total_s >= 3.9, f"сегменты не покрывают речь: Σ={total_s:.1f} с из 4 с"
    finally:
        server.should_exit = True
        th.join(timeout=5)
