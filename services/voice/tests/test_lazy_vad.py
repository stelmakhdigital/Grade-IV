"""Ленивая загрузка VAD (spec-18 #5): модель — при первом clone(), не на старте.

До первой загрузки health: {"vad": "not loaded"} (без ошибки); после — имя
провайдера. Провал загрузки кэшируется (антипаттерн B3: без кэша каждое
подключение ретраило бы дорогую загрузку), health показывает error.
"""
import threading

import pytest

from app.main import build_app
from app.providers import FakeSTT, FakeTTS
from app.stt_stream import EnergyVAD, LazyVAD


def test_lazy_not_loaded_then_ok() -> None:
    """До первого clone() — 'not loaded', после — имя провайдера."""
    lazy = LazyVAD(builder=lambda: EnergyVAD())
    assert lazy.name == "not loaded"
    per_conn = lazy.clone()
    assert lazy.name == "energy"
    assert per_conn.name == "energy"


def test_lazy_builds_once_and_clones_per_connection() -> None:
    """builder вызывается один раз; каждый clone() — независимый экземпляр."""
    calls = []

    def builder() -> EnergyVAD:
        calls.append(1)
        return EnergyVAD()

    lazy = LazyVAD(builder=builder)
    a, b = lazy.clone(), lazy.clone()
    assert a is not b
    assert len(calls) == 1, f"модель грузилась {len(calls)} раз"


def test_lazy_failure_cached() -> None:
    """Провал загрузки кэшируется: повторные clone() не ретраят загрузку."""
    calls = []

    def boom() -> EnergyVAD:
        calls.append(1)
        raise RuntimeError("onnx недоступен")

    lazy = LazyVAD(builder=boom)
    with pytest.raises(RuntimeError):
        lazy.clone()  # первая (и единственная) попытка загрузки
    assert lazy.name.startswith("error"), "health должен показать провал"
    with pytest.raises(RuntimeError):
        lazy.clone()
    assert len(calls) == 1, f"провал не кэширован: {len(calls)} попыток"
    assert lazy.name.startswith("error")


def test_lazy_threadsafe_single_load() -> None:
    """Параллельные первые подключения: одна загрузка под lock (не две)."""
    calls = []
    gate = threading.Event()

    def slow_builder() -> EnergyVAD:
        calls.append(1)
        gate.wait(timeout=5)  # держим загрузку, пока второй поток войдёт в clone
        return EnergyVAD()

    lazy = LazyVAD(builder=slow_builder)
    results: list[str] = []

    def worker():
        try:
            lazy.clone()
            results.append("ok")
        except Exception as exc:  # noqa: BLE001
            results.append(f"err: {exc}")

    threads = [threading.Thread(target=worker) for _ in range(2)]
    threads[0].start()
    threads[1].start()
    gate.set()
    for t in threads:
        t.join(timeout=10)
    assert results == ["ok", "ok"], results
    assert len(calls) == 1, f"параллельная загрузка x{len(calls)}"


def test_health_vad_not_loaded_without_model() -> None:
    """build_app с дефолтным (ленивым) VAD: health 'not loaded', модель не грузится."""
    from fastapi.testclient import TestClient

    app = build_app(stt=FakeSTT(), tts=FakeTTS())  # vad — LazyVAD по умолчанию
    assert app.state.vad.name == "not loaded"
    body = TestClient(app).get("/api/v1/health").json()
    assert body["vad"] == "not loaded"
