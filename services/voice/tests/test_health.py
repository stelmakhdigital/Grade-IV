"""Тесты health grade-voice (WP-1 каркас + WP-4 контракт).

Фейковые провайдеры — модели не требуются; реальные (env-режим) проверяем
только выбор по env и отсутствие ленивой загрузки при старте.
"""
import os

from fastapi.testclient import TestClient

from app.main import app, build_app
from app.providers import FakeSTT, FakeTTS

client = TestClient(build_app(FakeSTT(), FakeTTS()))


def test_health_fake() -> None:
    resp = client.get("/api/v1/health")
    assert resp.status_code == 200
    body = resp.json()
    assert body["service"] == "grade-voice"
    assert body["stt"]["provider"] == "fake"
    assert body["tts"]["provider"] == "fake"
    assert "ru_01" in body["tts"]["speakers"]
    # WP-4: расширенные поля health
    assert body["stt"]["loaded"] is False
    assert body["tts"]["loaded"] is False


def test_health_env_defaults_real_providers(monkeypatch) -> None:
    """Default-env: реальные провайдеры выбираются, но модели НЕ грузятся при старте."""
    for var in ("VOICE_STT_PROVIDER", "VOICE_TTS_PROVIDER", "STT_MODEL"):
        monkeypatch.delenv(var, raising=False)
    import importlib

    import app.main as main_mod

    importlib.reload(main_mod)
    client_real = TestClient(main_mod.app)
    body = client_real.get("/api/v1/health").json()
    assert body["stt"]["provider"] == "faster-whisper"
    assert body["stt"]["model"] == "small"
    assert body["stt"]["loaded"] is False  # lazy: модель скачается на первом /stt
    assert body["tts"]["provider"] == "silero"
    assert body["tts"]["model"] == "v5_ru"
    assert body["tts"]["loaded"] is False
    assert body["tts"]["speakers"]  # список спикеров известен до загрузки


def test_health_fake_env(monkeypatch) -> None:
    monkeypatch.setenv("VOICE_STT_PROVIDER", "fake")
    monkeypatch.setenv("VOICE_TTS_PROVIDER", "fake")
    import importlib

    import app.main as main_mod

    importlib.reload(main_mod)
    body = TestClient(main_mod.app).get("/api/v1/health").json()
    assert body["stt"]["provider"] == "fake"
    assert body["tts"]["provider"] == "fake"
    assert os.environ["VOICE_STT_PROVIDER"] == "fake"
