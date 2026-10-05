"""Стриминговый STT: WS /api/v1/stt/stream — PCM16 16 кГц кадры → partial/final/state.

Дизайн (ADR-007, задачи «стриминговый STT»):
- VAD — инкрементальная машина на окнах 512 сэмплов (32 мс @ 16 кГц):
  Silero (onnx, первичный) или энергетический (fallback, если onnx недоступен).
- Начало речи → ``{"type":"state","speech":true}``; конец → ``{"type":"state","speech":false}``
  и сразу после распознавания ``{"type":"final","text":...,"confidence":...}``.
  Финал распознаётся на ВЕСЬ буфер реплики (pre-roll → конец) — хвост не теряется.
- Пока речь идёт, не чаще раза в ~500 мс → ``{"type":"partial","text":...}``
  (распознавание на текущий момент — best-effort, не блокирует финал).
- Тишина без речи → ничего (ни partial, ни final).
- Контракт: клиент шлёт бинарные кадры PCM16 16 кГц mono (~250 мс); сервер —
  JSON-текст. Подключённые кадры во время тишины (до начала речи) не обрабатываются
  VAD-состоянием, кроме накопления pre-roll (~100 мс) для чистого старта слова.
"""

from __future__ import annotations

import asyncio
import json
import logging
import time
from collections import deque

import numpy as np
from fastapi import WebSocket

from .providers import SAMPLE_RATE, STTProvider

logger = logging.getLogger("grade-voice.stream")

# --- Именованные константы VAD (Silero) -------------------------------------
VAD_THRESHOLD = 0.5          # порог «речь»: prob ≥ 0.5
VAD_MIN_SILENCE_MS = 600     # тишина ≥ 600 мс после речи → конец реплики (final)
VAD_MIN_SPEECH_MS = 250      # речь короче 250 мс → всплеск, без final
PARTIAL_INTERVAL_S = 0.5     # partial не чаще раза в 500 мс
WINDOW = 512                 # окно Silero, сэмплы @ 16 кГц (32 мс)
PREROLL_SAMPLES = 1600       # pre-roll перед стартом слова (100 мс)
ENERGY_RMS = 0.005           # энергетический VAD: rms-порог (≈ -46 дБFS)

__all__ = [
    "VAD_THRESHOLD", "VAD_MIN_SILENCE_MS", "VAD_MIN_SPEECH_MS",
    "PARTIAL_INTERVAL_S", "WINDOW", "PREROLL_SAMPLES", "ENERGY_RMS",
    "SileroVAD", "EnergyVAD", "StreamVAD", "build_vad", "register_stt_stream",
]


class SileroVAD:
    """Silero VAD (onnx, bundled в faster-whisper) — первичный провайдер.

    Окно 512 сэмплов; для непрерывности контекста RNN предыдущее окно
    передаётся как контекст (вход 1024 → берём prob второго окна).
    """

    name = "silero"

    def __init__(
        self,
        threshold: float = VAD_THRESHOLD,
        min_silence_ms: int = VAD_MIN_SILENCE_MS,
        min_speech_ms: int = VAD_MIN_SPEECH_MS,
    ) -> None:
        self.threshold = threshold
        self.min_silence_ms = min_silence_ms
        self.min_speech_ms = min_speech_ms
        from faster_whisper.vad import get_vad_model

        self._model = get_vad_model()
        self._prev = np.zeros(WINDOW, dtype=np.float32)  # контекст (предыдущее окно)

    def clone(self) -> "SileroVAD":
        """Per-connection экземпляр (контекст изолирован, модель общая)."""
        v = SileroVAD.__new__(SileroVAD)
        v.threshold = self.threshold
        v.min_silence_ms = self.min_silence_ms
        v.min_speech_ms = self.min_speech_ms
        v._model = self._model  # onnx-сессия потокобезопасна (read-only, свой h/c на вызов)
        v._prev = np.zeros(WINDOW, dtype=np.float32)
        return v

    def prob(self, window: np.ndarray) -> float:
        x = np.concatenate([self._prev, window]).astype(np.float32)
        out = self._model(x)
        self._prev = window.copy()
        return float(np.asarray(out).reshape(-1)[-1])


class EnergyVAD:
    """Энергетический VAD (fallback): rms окна ≥ порога → речь (prob 1.0)."""

    name = "energy"

    def __init__(
        self,
        threshold: float = VAD_THRESHOLD,
        min_silence_ms: int = VAD_MIN_SILENCE_MS,
        min_speech_ms: int = VAD_MIN_SPEECH_MS,
        energy_rms: float = ENERGY_RMS,
    ) -> None:
        self.threshold = threshold
        self.min_silence_ms = min_silence_ms
        self.min_speech_ms = min_speech_ms
        self.energy_rms = energy_rms

    def clone(self) -> "EnergyVAD":
        return EnergyVAD(
            threshold=self.threshold,
            min_silence_ms=self.min_silence_ms,
            min_speech_ms=self.min_speech_ms,
            energy_rms=self.energy_rms,
        )

    def prob(self, window: np.ndarray) -> float:
        rms = float(np.sqrt(np.mean(window.astype(np.float32) ** 2)))
        return 1.0 if rms >= self.energy_rms else 0.0


def build_vad():
    """VAD по умолчанию: Silero (onnx); при недоступности — энергетический."""
    try:
        return SileroVAD()
    except Exception as exc:  # ImportError onnxruntime / битый onnx
        logger.warning("Silero VAD недоступен (%s) — fallback на energy VAD", exc)
        return EnergyVAD()


class StreamVAD:
    """Инкрементальная VAD-машина: 512-сэмпловые окна → события речи.

    События: ``("start", abs_start)`` / ``("end", abs_start, abs_end)`` —
    позиции в сэмплах всего потока соединения.
    """

    def __init__(self, vad, preroll_samples: int = PREROLL_SAMPLES) -> None:
        self._vad = vad
        self._min_silence = vad.min_silence_ms * SAMPLE_RATE // 1000
        self._min_speech = vad.min_speech_ms * SAMPLE_RATE // 1000
        self._preroll = preroll_samples
        self._rest = np.zeros(0, dtype=np.float32)
        self._total = 0  # сэмплы, обработанные окнами
        self._in_speech = False
        self._start = 0
        self._silence_since = 0

    def feed(self, samples: np.ndarray) -> list[tuple[str, int, int]]:
        events: list[tuple[str, int, int]] = []
        x = np.concatenate([self._rest, samples]) if self._rest.size else samples
        nwin = len(x) // WINDOW
        for i in range(nwin):
            pos = self._total + i * WINDOW
            p = self._vad.prob(x[i * WINDOW:(i + 1) * WINDOW])
            if not self._in_speech:
                if p >= self._vad.threshold:
                    self._in_speech = True
                    self._start = max(0, pos - self._preroll)
                    events.append(("start", self._start, 0))
            else:
                if p >= self._vad.threshold:
                    self._silence_since = 0
                else:
                    if self._silence_since == 0:
                        self._silence_since = pos
                    if pos - self._silence_since >= self._min_silence:
                        end = self._silence_since
                        self._in_speech = False
                        self._silence_since = 0
                        if end - self._start >= self._min_speech:
                            events.append(("end", self._start, end))
        self._rest = x[nwin * WINDOW:]
        self._total += nwin * WINDOW
        return events


# --- WS-эндпоинт ------------------------------------------------------------

async def _send(ws: WebSocket, payload: dict) -> None:
    try:
        await ws.send_text(json.dumps(payload, ensure_ascii=False))
    except Exception:
        pass  # клиент отключился — best-effort


async def _do_partial(stt: STTProvider, ws: WebSocket, pcm: bytes, cell: dict) -> None:
    """Partial (best-effort): распознавание на текущий буфер; дубли не шлём."""
    try:
        res = await asyncio.to_thread(stt.transcribe, pcm, SAMPLE_RATE)
        if res.text and res.text != cell.get("text"):
            cell["text"] = res.text
            await _send(ws, {"type": "partial", "text": res.text})
    except Exception:
        pass  # partial — не критичен
    finally:
        cell["busy"] = False


def register_stt_stream(app, stt: STTProvider, vad) -> None:
    """Регистрирует WS /api/v1/stt/stream (контракт — докстрока модуля)."""

    @app.websocket("/api/v1/stt/stream")
    async def stt_stream_endpoint(ws: WebSocket) -> None:
        await ws.accept()
        engine = StreamVAD(vad.clone())  # состояние VAD — per-connection
        ring: deque[bytes] = deque(maxlen=4)  # pre-roll: последние кадры (≤1 с)
        buf = bytearray()  # аудио текущей реплики (от pre-roll)
        speech = False
        cell = {"busy": False, "text": ""}  # состояние partial-воркера

        async for msg in ws.iter_bytes():
            if len(msg) < 2 or len(msg) % 2:
                continue
            samples = np.frombuffer(msg, dtype=np.int16).astype(np.float32) / 32768
            ring.append(bytes(msg))

            for kind, a, b in engine.feed(samples):
                if kind == "start":
                    speech = True
                    # pre-roll: предыдущий кадр (≤250 мс) — чистый старт слова.
                    buf = bytearray(ring[-2] if len(ring) >= 2 else b"")
                    await _send(ws, {"type": "state", "speech": True})
                elif kind == "end":
                    speech = False
                    audio = bytes(buf) + msg  # весь буфер + кадр с концом (хвост ≤250 мс)
                    buf = bytearray()
                    speech_ms = round((b - a) * 1000 / SAMPLE_RATE)  # длительность речи по VAD
                    await _send(ws, {"type": "state", "speech": False})
                    res = await asyncio.to_thread(stt.transcribe, audio, SAMPLE_RATE)
                    await _send(ws, {
                        "type": "final",
                        "text": res.text,
                        "confidence": round(res.confidence, 3),
                        "speech_ms": speech_ms,
                    })

            if speech:
                buf.extend(msg)
                if not cell["busy"] and (time.monotonic() - cell.get("t", 0.0)) >= PARTIAL_INTERVAL_S:
                    cell["busy"] = True
                    cell["t"] = time.monotonic()
                    snap = bytes(buf)
                    asyncio.get_running_loop().create_task(_do_partial(stt, ws, snap, cell))
