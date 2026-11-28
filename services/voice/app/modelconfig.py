"""Переопределение моделей из файла + каталог того, что реально лежит на диске.

Зачем (фидбек 2026-11-28): выбор модели жил только в окружении, и чтобы
поставить другую модель STT/TTS, оператор правил .env и перезапускал сервис, а
узнать, почему интервьюер молчит, было негде — сервис отвечает 200 с
loaded=false.

Порядок старшинства (сверху вниз): переменная окружения > файл > дефолт кода.
env выигрывает специально: docker-compose и run-all.sh задают модель явно, и
файл не должен тихо переопределять то, что объявлено деплоем (правило проекта:
дефолт STT — gigaam, см. AGENTS.md). Поэтому настройка, закреплённая env, в UI
показывается закреплённой, а не «сохраните и оно применится».

Файл применяется перезапуском сервиса: gigaam держит веса в памяти (лимит
voice в prod — 8 GiB), вторая модель рядом не влезает, а цена неудачной
подмены посреди интервью — молчащий интервьюер.
"""
from __future__ import annotations

import json
import os
from pathlib import Path

from pydantic import BaseModel, Field

__all__ = ["ModelConfig", "CATALOG", "config_path"]

#: Что предлагаем выбрать. Имена — те же, что перечислены в .env.example;
#: доступность проверяется по файлам на диске, а не по этому списку.
CATALOG: dict = {
    "stt": {
        "providers": ["gigaam", "faster-whisper", "fake"],
        "models": {
            "gigaam": ["v3_e2e_rnnt", "v3_rnnt", "v3_ctc", "v3_e2e_ctc"],
            "faster-whisper": ["tiny", "base", "small", "mid", "large-v3"],
            "fake": ["fake"],
        },
    },
    "tts": {
        "providers": ["silero", "fake"],
        "models": {"silero": ["v5_ru"], "fake": ["fake"]},
    },
}


def config_path() -> Path:
    """Путь к файлу переопределения (VOICE_MODELS_FILE, иначе config/voice-models.json)."""
    return Path(os.getenv("VOICE_MODELS_FILE") or "config/voice-models.json")


class ModelConfig:
    """Чтение/запись файла + разрешение (env > файл > дефолт) с источником значения."""

    def __init__(self, path: Path | None = None) -> None:
        # Путь разбираем при каждом обращении, а не в конструкторе: env может
        # появиться позже импорта модуля (тесты, обёртки запуска).
        self._path = path

    @property
    def path(self) -> Path:
        return self._path or config_path()

    # ------------------------------------------------------------------ чтение
    def read(self) -> dict:
        """Файл как есть; нет файла / битый JSON — пустой словарь (не падать)."""
        try:
            raw = json.loads(self.path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            return {}
        return raw if isinstance(raw, dict) else {}

    def resolve(self, kind: str, field: str, env_var: str, default: str) -> tuple[str, str]:
        """(значение, источник) для kind.field: env > файл > дефолт кода.

        Источник возвращается специально: UI показывает «закреплено env»,
        иначе сохранение в файл при заданной переменной выглядит сломанным.
        """
        env = (os.getenv(env_var) or "").strip()
        if env:
            return env, "env"
        val = (self.read().get(kind, {}) or {}).get(field)
        if isinstance(val, str) and val.strip():
            return val.strip(), "file"
        return default, "default"

    # ------------------------------------------------------------------ запись
    def write(self, payload: dict) -> dict:
        """Атомичная запись файла целиком (валидированный payload)."""
        self.path.parent.mkdir(parents=True, exist_ok=True)
        tmp = self.path.with_suffix(self.path.suffix + ".tmp")
        tmp.write_text(json.dumps(payload, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        os.replace(tmp, self.path)
        return payload


def available_models(kind: str, provider: str) -> list[dict]:
    """Кандидаты провайдера с фактом «лежит на диске».

    UI по этому поляет недоступные: выбрать модель, весов которой нет, значит
    получить молчащий сервис на следующем интервью (запуск скачивания в
    рантайке мёртв — у Sber-CDN просрочен сертификат).
    """
    names = CATALOG[kind]["models"].get(provider, [])
    if provider == "fake":
        return [{"name": n, "present": True, "path": ""} for n in names]
    out = []
    for name in names:
        paths = _weights_for(kind, provider, name)
        present = bool(paths) and all(p.exists() for p in paths)
        out.append({
            "name": name,
            "present": present,
            "path": str(paths[0]) if paths else "",
            "missing": [str(p) for p in paths if not p.exists()],
        })
    return out


def _weights_for(kind: str, provider: str, name: str) -> list[Path]:
    """Пути весов кандидата — те же функции, что у самих провайдеров."""
    if kind == "stt" and provider == "gigaam":
        from .gigaam import weights_for

        return weights_for(name)
    if kind == "stt" and provider == "faster-whisper":
        from .faster_whisper import weights_for

        return weights_for(name)
    if kind == "tts" and provider == "silero":
        from .silero import weights_for

        return weights_for(name)
    return []


# ------------------------------------------------------------- админ-контур

class ModelSel(BaseModel):
    """STT: провайдер + имя модели."""

    provider: str = Field(min_length=1, max_length=40)
    model: str = Field(min_length=1, max_length=80)


class SpeakerSel(BaseModel):
    """TTS: провайдер + спикер (модель Silero одна, различаются голоса)."""

    provider: str = Field(min_length=1, max_length=40)
    speaker: str = Field(min_length=1, max_length=40)


class ModelConfigPayload(BaseModel):
    """Тело PUT /api/v1/config/models."""

    stt: ModelSel
    tts: SpeakerSel


def _sources(cfg: ModelConfig) -> dict:
    """Текущие значения с источником (env | file | default) и именем переменной.

    UI показывает «закреплено переменной» именно по source=env: иначе сохранение
    в файл при заданной переменной выглядит сломанным (файл env не перебивает).
    """
    from .silero import DEFAULT_MODEL_SPEAKER

    def one(kind: str, field: str, env_var: str, default: str) -> dict:
        value, source = cfg.resolve(kind, field, env_var, default)
        return {"value": value, "source": source, "env": env_var}

    stt_model = {
        # У каждого STT-провайдера своя переменная имени модели.
        "gigaam": one("stt", "model", "GIGAAM_MODEL", "v3_e2e_rnnt"),
        "faster-whisper": one("stt", "model", "STT_MODEL", "small"),
    }
    return {
        "stt": {"provider": one("stt", "provider", "VOICE_STT_PROVIDER", "gigaam"),
                "model": stt_model},
        "tts": {"provider": one("tts", "provider", "VOICE_TTS_PROVIDER", "silero"),
                "speaker": one("tts", "speaker", "TTS_SPEAKER", DEFAULT_MODEL_SPEAKER)},
    }


def register_model_config(app, cfg: ModelConfig, stt, tts) -> None:
    """GET/PUT /api/v1/config/models — контур api (админка), не кандидата.

    GET отвечает даже без токена: половина ценности — увидеть, что подключено и чего
    не хватает на диске. Запись закрыта VOICE_ADMIN_TOKEN: voice живёт в ЛВС без
    своей аутентификации, а испорченный выбор моделей = молчащий интервьюер на
    следующем интервью. Без токена — 503, как у sandbox /admin/tasks (функция
    выключена, а не работает незащищённо).
    """
    from fastapi import Header, HTTPException

    def _require_token(token: str | None) -> None:
        want = os.getenv("VOICE_ADMIN_TOKEN", "")
        if not want:
            raise HTTPException(status_code=503, detail={
                "code": "config_readonly",
                "message": "запись настроек выключена: задайте переменную VOICE_ADMIN_TOKEN у voice",
            })
        if token != want:
            raise HTTPException(status_code=401, detail={"code": "unauthorized", "message": "неверный токен"})

    @app.get("/api/v1/config/models")
    def config_models_get() -> dict:
        stt_p, tts_p = stt.health(), tts.health()
        return {
            "path": str(cfg.path),
            "current": {
                "stt": {"provider": stt_p.get("provider"), "model": stt_p.get("model"),
                        "device": stt_p.get("device"), "loaded": stt_p.get("loaded")},
                "tts": {"provider": tts_p.get("provider"), "model": tts_p.get("model"),
                        "loaded": tts_p.get("loaded"), "speakers": tts_p.get("speakers")},
            },
            "sources": _sources(cfg),
            "catalog": {
                "stt": {prov: available_models("stt", prov) for prov in CATALOG["stt"]["providers"]},
                "tts": {prov: available_models("tts", prov) for prov in CATALOG["tts"]["providers"]},
            },
            "writable": os.getenv("VOICE_ADMIN_TOKEN", "") != "",
            "applies": "restart",
            "applies_note": "модель загружается при старте voice — сохранённый выбор "
                            "применится после перезапуска сервиса",
        }

    @app.put("/api/v1/config/models")
    def config_models_put(payload: ModelConfigPayload,
                          x_admin_token: str | None = Header(default=None)) -> dict:
        # Порядок важен: без токена ответ 503 при любом теле, иначе «выключено»
        # превращается в 400 и подсказывает содержимое неавторизованному.
        _require_token(x_admin_token)
        for kind, sel in (("stt", payload.stt), ("tts", payload.tts)):
            prov = sel.provider.strip().lower()
            if prov not in CATALOG[kind]["providers"]:
                raise HTTPException(status_code=400, detail={
                    "code": "validation",
                    "message": f"{kind}: провайдера {sel.provider!r} нет; известны: "
                               + ", ".join(CATALOG[kind]["providers"]),
                })
        # Имя модели проверяем по каталогу провайдера; спикера TTS — по списку
        # голосов загруженной модели (до загрузки берём известную пятёрку v5_ru).
        stt_known = CATALOG["stt"]["models"][payload.stt.provider.strip().lower()]
        if payload.stt.model.strip() not in stt_known:
            raise HTTPException(status_code=400, detail={
                "code": "validation",
                "message": "stt: модели " + repr(payload.stt.model) + " нет; известны: "
                           + ", ".join(stt_known),
            })
        speakers = tts.health().get("speakers") or []
        if payload.tts.provider.strip().lower() == "silero" and speakers and \
                payload.tts.speaker.strip() not in speakers:
            raise HTTPException(status_code=400, detail={
                "code": "validation",
                "message": "tts: спикера " + repr(payload.tts.speaker) + " нет в модели; известны: "
                           + ", ".join(speakers),
            })
        saved = cfg.write({
            "stt": {"provider": payload.stt.provider.strip().lower(),
                    "model": payload.stt.model.strip()},
            "tts": {"provider": payload.tts.provider.strip().lower(),
                    "speaker": payload.tts.speaker.strip()},
        })
        # Модель без весов — не 400 (оператор может доложить файлы позже), но и
        # молчать нельзя: такой провайдер будет молчать на интервью.
        missing = {}
        for kind, name, prov in (("stt", payload.stt.model, payload.stt.provider),
                                 ("tts", payload.tts.speaker, payload.tts.provider)):
            for m in available_models(kind, prov.strip().lower()):
                if m["name"] == name.strip() and not m["present"]:
                    missing[f"{kind}:{name.strip()}"] = m["missing"]
        return {"saved": saved, "applies": "restart", "missing_weights": missing}
