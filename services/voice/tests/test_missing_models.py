"""Старт без весов на диске: отказ должен быть слышен в логе.

Без проверки voice поднимается с loaded=false, STT по контракту отдаёт text=""
(не 500), TTS молчит — «интервьюер не отвечает» вместо «не скачали модель».
"""
import logging

from app.gigaam import GigaAMSTT
from app.main import _warn_missing_models
from app.providers import FakeSTT, FakeTTS
from app.silero import SileroTTS


def _errors(caplog, provider):
    with caplog.at_level(logging.ERROR, logger="app"):
        _warn_missing_models(provider)
    return [r.message for r in caplog.records if r.levelno >= logging.ERROR]


def test_молчит_на_фейковых_провайдерах(caplog):
    """FakeSTT/FakeTTS весов не имеют — ложных тревог на старте быть не должно."""
    assert _errors(caplog, FakeSTT()) == []
    assert _errors(caplog, FakeTTS()) == []


def test_gigaam_без_весов_пишет_error_с_путём(caplog, tmp_path, monkeypatch):
    monkeypatch.setenv("GIGAAM_DOWNLOAD_ROOT", str(tmp_path / "нет-такой-папки"))
    msgs = _errors(caplog, GigaAMSTT())
    assert any("v3_e2e_rnnt.ckpt" in m for m in msgs), msgs
    assert any("download-models.sh" in m for m in msgs), msgs


def test_gigaam_с_весами_молчит(caplog, tmp_path, monkeypatch):
    root = tmp_path / "gigaam"
    (root / "tokenizer").mkdir(parents=True)
    (root / "v3_e2e_rnnt.ckpt").write_bytes(b"x")
    monkeypatch.setenv("GIGAAM_DOWNLOAD_ROOT", str(root))
    assert _errors(caplog, GigaAMSTT()) == []


def test_silero_без_весов_пишет_error_с_путём(caplog, tmp_path, monkeypatch):
    monkeypatch.setenv("TTS_MODEL_DIR", str(tmp_path / "tts"))
    msgs = _errors(caplog, SileroTTS())
    assert any("silero-tts-" in m for m in msgs), msgs
