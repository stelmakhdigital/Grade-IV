"""Настройки моделей: GET/PUT /api/v1/config/models и старшинство env > файл > дефолт.

Фидбек 2026-11-28: модель выбиралась только переменными окружения, а «что
подключено и почему интервьюер молчит» было негде увидеть.
"""
import json

import pytest
from fastapi.testclient import TestClient

from app.main import build_app
from app.modelconfig import ModelConfig
from app.providers import FakeSTT, FakeTTS

URL = "/" + "api/v1/config/models"


@pytest.fixture()
def cfg_file(tmp_path, monkeypatch):
    path = tmp_path / "voice-models.json"
    monkeypatch.setenv("VOICE_MODELS_FILE", str(path))
    monkeypatch.delenv("VOICE_ADMIN_TOKEN", raising=False)
    for var in ("VOICE_STT_PROVIDER", "GIGAAM_MODEL", "STT_MODEL", "VOICE_TTS_PROVIDER",
                "TTS_SPEAKER", "GIGAAM_DOWNLOAD_ROOT", "STT_DOWNLOAD_ROOT", "TTS_MODEL_DIR",
                "TTS_MODEL_PATH"):
        monkeypatch.delenv(var, raising=False)
    return path


#: Спикер из health() фейкового TTS: валидатор сверяется со списком голосов модели.
SPEAKER = FakeTTS().health().get("speakers", ["ru_01"])[0]


def client(tmp_path=None):
    return TestClient(build_app(stt=FakeSTT(), tts=FakeTTS(), vad=FakeVAD()))


class FakeVAD:
    name = "energy-fake"

    def clone(self):
        return self


# --------------------------------------------------------------------- GET
def test_get_показывает_источник_и_недоступные_модели(cfg_file) -> None:
    body = client().get(URL).json()
    assert body["current"]["stt"]["provider"] == "fake"
    assert body["sources"]["stt"]["provider"]["source"] == "default"
    assert body["sources"]["stt"]["provider"]["env"] == "VOICE_STT_PROVIDER"
    # Каталог gigaam: модели перечислены, весов в пустом каталоге нет present=false.
    gigaam = body["catalog"]["stt"]["gigaam"]
    assert {m["name"] for m in gigaam} >= {"v3_e2e_rnnt", "v3_ctc"}
    assert all(m["present"] is False and m["missing"] for m in gigaam)
    # Писать нельзя без токена — но прочитать можно (половина ценности — диагностика).
    assert body["writable"] is False and body["applies"] == "restart"


def test_get_весы_на_диске_видны(cfg_file, tmp_path, monkeypatch) -> None:
    root = tmp_path / "gigaam"
    root.mkdir()
    (root / "v3_e2e_rnnt.ckpt").write_bytes(b"x")
    monkeypatch.setenv("GIGAAM_DOWNLOAD_ROOT", str(root))
    gigaam = {m["name"]: m for m in client().get(URL).json()["catalog"]["stt"]["gigaam"]}
    assert gigaam["v3_e2e_rnnt"]["present"] is False  # токенизатора нет
    assert gigaam["v3_e2e_rnnt"]["missing"] == [str(root / "v3_e2e_rnnt_tokenizer.model")]
    (root / "v3_e2e_rnnt_tokenizer.model").write_bytes(b"y")
    gigaam = {m["name"]: m for m in client().get(URL).json()["catalog"]["stt"]["gigaam"]}
    assert gigaam["v3_e2e_rnnt"]["present"] is True


# --------------------------------------------------------------------- PUT
def test_put_без_токена_503(cfg_file) -> None:
    r = client().put(URL, json={
        "stt": {"provider": "gigaam", "model": "v3_e2e_rnnt"},
        "tts": {"provider": "silero", "speaker": SPEAKER},
    })
    assert r.status_code == 503 and r.json()["detail"]["code"] == "config_readonly"
    assert not cfg_file.exists()  # «выключено» не значит «записано»


def test_put_валидация_и_запись(cfg_file, monkeypatch) -> None:
    monkeypatch.setenv("VOICE_ADMIN_TOKEN", "s3cret")
    c = client()
    bad = c.put(URL, headers={"X-Admin-Token": "s3cret"}, json={
        "stt": {"provider": "gigaam", "model": "нет-такой-модели"},
        "tts": {"provider": "silero", "speaker": SPEAKER},
    })
    assert bad.status_code == 400 and "v3_e2e_rnnt" in bad.json()["detail"]["message"]

    wrong = c.put(URL, headers={"X-Admin-Token": "wrong"}, json={
        "stt": {"provider": "gigaam", "model": "v3_e2e_ctc"},
        "tts": {"provider": "silero", "speaker": SPEAKER},
    })
    assert wrong.status_code == 401

    ok = c.put(URL, headers={"X-Admin-Token": "s3cret"}, json={
        "stt": {"provider": "gigaam", "model": "v3_e2e_ctc"},
        "tts": {"provider": "silero", "speaker": SPEAKER},
    })
    assert ok.status_code == 200, ok.text
    saved = json.loads(cfg_file.read_text(encoding="utf-8"))
    assert saved["stt"] == {"provider": "gigaam", "model": "v3_e2e_ctc"}
    assert saved["tts"] == {"provider": "silero", "speaker": SPEAKER}
    # Выбранную модель предупреждаем, а не отвергаем: весов нет — будет молчать.
    assert "stt:v3_e2e_ctc" in ok.json()["missing_weights"]


# ------------------------------------------------------- старшинство значений
def test_старшинство_env_файл_дефолт(cfg_file, monkeypatch) -> None:
    cfg = ModelConfig(cfg_file)
    assert cfg.resolve("stt", "provider", "VOICE_STT_PROVIDER", "gigaam") == ("gigaam", "default")

    cfg_file.write_text(json.dumps({"stt": {"provider": "faster-whisper", "model": "large-v3"}}),
                        encoding="utf-8")
    assert cfg.resolve("stt", "provider", "VOICE_STT_PROVIDER", "gigaam") == ("faster-whisper", "file")
    assert cfg.resolve("stt", "model", "STT_MODEL", "small") == ("large-v3", "file")

    monkeypatch.setenv("VOICE_STT_PROVIDER", "gigaam")
    monkeypatch.setenv("STT_MODEL", "base")
    # env выигрывает: деплой (docker-compose, run-all.sh) не должен переопределяться
    # файлом, иначе admin-правка тихо меняет прод.
    assert cfg.resolve("stt", "provider", "VOICE_STT_PROVIDER", "gigaam") == ("gigaam", "env")
    assert cfg.resolve("stt", "model", "STT_MODEL", "small") == ("base", "env")


def test_битый_файл_не_роняет(cfg_file) -> None:
    cfg_file.write_text("{не json", encoding="utf-8")
    body = client().get(URL).json()
    assert body["sources"]["stt"]["provider"]["source"] == "default"
