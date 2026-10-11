"""GigaAM STT-провайдер (v3 CTC/RNNT, Sber, MIT).

Юнит-тесты — детерминированные (без загрузки модели): валидация env, пустое/
битое аудио, health до загрузки, выбор провайдера в _build_stt.
Тест с реальной моделью — маркер e2e, skip если .ckpt нет в models/stt-gigaam
(модель ~420 МБ, скачивается scripts/download-models.sh / вручную с CDN Sber).
"""
import os

import numpy as np
import pytest

from app.gigaam import GigaAMSTT, MODEL_NAMES
from app.providers import SAMPLE_RATE, extract_pcm16

MODELS_DIR = os.path.join(os.path.dirname(__file__), "..", "models", "stt-gigaam")


# ---------------------------------------------------------------- unit (no model)
def test_model_names_contains_v3_defaults():
    for m in ("v3_rnnt", "v3_ctc", "v2_ctc"):
        assert m in MODEL_NAMES


def test_unknown_model_falls_back_to_v3_e2e_rnnt():
    p = GigaAMSTT(model_name="definitely_not_a_model")
    assert p.name == "gigaam"
    assert p._model_name == "v3_e2e_rnnt"


def test_env_model_name(monkeypatch):
    monkeypatch.setenv("GIGAAM_MODEL", "v3_ctc")
    p = GigaAMSTT()
    assert p._model_name == "v3_ctc"


def test_empty_audio_no_model_load():
    p = GigaAMSTT()
    res = p.transcribe(b"", 16000)
    assert res.text == ""
    assert res.confidence == 0.0
    assert p.loaded is False  # модель не грузится на пустом входе


def test_garbage_riff_header_returns_empty():
    # RIFF/WAVE заголовок с битым fmt-чанком (не PCM16 mono) -> extract_pcm16 None
    bad = b"RIFF" + (60).to_bytes(4, "little") + b"WAVE" + b"fmt " + b"\x00" * 36
    assert extract_pcm16(bad) is None
    p = GigaAMSTT()
    res = p.transcribe(bad, 16000)
    assert res.text == ""
    assert p.loaded is False


def test_health_before_load():
    p = GigaAMSTT(model_name="v3_ctc", device="cpu")
    h = p.health()
    assert h["provider"] == "gigaam"
    assert h["model"] == "v3_ctc"
    assert h["device"] == "cpu"
    assert h["loaded"] is False


def test_build_stt_selects_gigaam(monkeypatch):
    monkeypatch.setenv("VOICE_STT_PROVIDER", "gigaam")
    from app.main import _build_stt

    p = _build_stt()
    assert isinstance(p, GigaAMSTT)


def test_build_stt_unknown_provider_raises(monkeypatch):
    monkeypatch.setenv("VOICE_STT_PROVIDER", "nonexistent-provider")
    from app.main import _build_stt

    with pytest.raises(ValueError):
        _build_stt()


# ------------------------------------------------------------------ e2e (model)
@pytest.mark.e2e
@pytest.mark.skipif(
    not os.path.isfile(os.path.join(MODELS_DIR, "v3_rnnt.ckpt")),
    reason="модель v3_rnnt.ckpt не скачана (models/stt-gigaam)",
)
def test_gigaam_real_transcribes_speech():
    """v3_rnnt: fixture-речь -> осмысленный русский текст (CPU, ~0.1 с/3 с)."""
    p = GigaAMSTT(model_name="v3_rnnt", device="cpu", download_root=MODELS_DIR)
    pcm = open(
        os.path.join(os.path.dirname(__file__), "fixtures", "speech_honest.pcm"), "rb"
    ).read()
    res = p.transcribe(pcm, 16000)
    assert res.text, "fixture-речь распознана как тишина"
    assert "здравствуйте" in res.text.lower(), res.text
    assert res.confidence > 0.5
    assert res.duration_s == pytest.approx(3.0, abs=0.1)
    assert p.health()["loaded"] is True


@pytest.mark.e2e
@pytest.mark.skipif(
    not os.path.isfile(os.path.join(MODELS_DIR, "v3_rnnt.ckpt")),
    reason="модель v3_rnnt.ckpt не скачана (models/stt-gigaam)",
)
def test_gigaam_real_silence_empty():
    """Тишина 2 с -> text='' (CTC/RNNT не галлюцинируют на тишине)."""
    p = GigaAMSTT(model_name="v3_rnnt", device="cpu", download_root=MODELS_DIR)
    res = p.transcribe(np.zeros(SAMPLE_RATE * 2, dtype=np.int16).tobytes(), 16000)
    assert res.text == ""
    assert res.confidence == 0.0
