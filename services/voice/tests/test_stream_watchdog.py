"""spec-14/B2: idle-watchdog WS-стримов (/stt/stream, /vad/stream).

Мёртвый клиент (полуоткрытый TCP: обрыв сети, kill -9) не порождает ошибку
в ``ws.iter_bytes()`` — раньше соединение висело неограниченно, удерживая
per-connection VAD-состояние и слот однопоточного STT_EXECUTOR. Теперь при
отсутствии входящих кадров STREAM_IDLE_TIMEOUT_S сервер сам закрывает
соединение с close code 1001 (going away), отменяя pending-таски.

Тест: реальный uvicorn + websockets-клиент; таймаут занижаем monkeypatch'ем
(watchdog читает константу модуля в рантайме), подключаемся и МОЛЧИМ —
сервер должен закрыть соединение с кодом 1001 за ~таймаут.
"""
import asyncio
import socket
import threading
import time

import pytest
import uvicorn
import websockets

import app.stt_stream as stt_stream_mod
from app.main import build_app
from app.providers import FakeTTS
from app.stt_stream import EnergyVAD

from test_stt_stream import LengthSTT, _free_port


@pytest.fixture()
def idle_uri(monkeypatch):
    """uvicorn с заниженным idle-таймаутом (0.5 с) — корень сервиса."""
    monkeypatch.setattr(stt_stream_mod, "STREAM_IDLE_TIMEOUT_S", 0.5)
    app = build_app(
        stt=LengthSTT(), tts=FakeTTS(),
        vad=EnergyVAD(min_silence_ms=150, min_speech_ms=100),
    )
    port = _free_port()
    server = uvicorn.Server(uvicorn.Config(app, host="127.0.0.1", port=port, log_level="error"))
    th = threading.Thread(target=server.run, daemon=True)
    th.start()
    deadline = time.time() + 10
    while not server.started and time.time() < deadline:
        time.sleep(0.05)
    assert server.started, "uvicorn не стартовал"
    yield f"ws://127.0.0.1:{port}"
    server.should_exit = True
    th.join(timeout=5)


def _wait_idle_close(uri: str) -> float:
    """Подключаемся и молчим → сервер закрывает соединение (1001). Возврат: elapsed."""
    async def scenario():
        async with websockets.connect(uri, max_size=None) as ws:
            t0 = time.monotonic()
            try:
                while True:
                    await asyncio.wait_for(ws.recv(), timeout=10)
            except asyncio.TimeoutError:
                pytest.fail("соединение не закрыто сервером за 10 с (watchdog не сработал)")
            except websockets.exceptions.ConnectionClosed as exc:
                close = exc.rcvd or exc.sent
                assert close is not None and close.code == 1001, \
                    f"close {close and close.code}, ожидался 1001"
            return time.monotonic() - t0

    return asyncio.run(scenario())


def test_stt_stream_idle_closed_1001(idle_uri: str) -> None:
    """Молчащий клиент: сервер сам закрывает /stt/stream с кодом 1001 за ~таймаут."""
    elapsed = _wait_idle_close(idle_uri + "/api/v1/stt/stream")
    assert elapsed < 10, f"close 1001 не пришёл за таймаут (elapsed={elapsed:.1f}с)"


def test_vad_stream_idle_closed_1001(idle_uri: str) -> None:
    """Аналогично для /vad/stream (мёртвый клиент держал VAD-состояние)."""
    elapsed = _wait_idle_close(idle_uri + "/api/v1/vad/stream")
    assert elapsed < 10, f"close 1001 не пришёл за таймаут (elapsed={elapsed:.1f}с)"


def test_active_client_not_disconnected(idle_uri: str) -> None:
    """Живой клиент (кадры тишины идут) НЕ отключается watchdog'ом: close code
    остаётся нашим (1000), а не 1001 от сервера."""
    async def scenario():
        async with websockets.connect(idle_uri + "/api/v1/stt/stream", max_size=None) as ws:
            silence = bytes(500)  # PCM16 тишина (rms < порога — не речь)
            deadline = time.monotonic() + 2.0  # > 4× idle-таймаута (0.5с)
            while time.monotonic() < deadline:
                await ws.send(silence)
                await asyncio.sleep(0.2)
            await ws.close(code=1000)
            # если бы сервер закрыл нас watchdog'ом — close_code был бы 1001
            assert ws.close_code == 1000, f"соединение закрыл сервер (code {ws.close_code})"
    asyncio.run(scenario())
