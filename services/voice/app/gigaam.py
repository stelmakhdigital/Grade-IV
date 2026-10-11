"""STT-провайдер: GigaAM (SberDevices, MIT) — v3 CTC/RNNT, русский.

Почему (решение 2026-10-11): GigaAM v3 на русском даёт в ~2 раза меньший WER,
чем Whisper large-v3 (официальная таблица salute-developers/GigaAM evaluation.md:
v3 RNNT 8.3 / v3 CTC 9.1 / v2 CTC 11.1 vs Whisper large-v3 21.0 — average WER по
10 валидационным наборам, включая callcenter/phone/natural speech). N-gram LM
rescoring не нужен: v3 RNNT уже несёт встроенную языковую модель (эффективнее
внешнего rescoring), v3 CTC — жадный CTC-декодер.

Модель: .ckpt с CDN Sber (https://cdn.chatwm.opensmodel.sberdevices.ru/GigaAM),
кэш — ``GIGAAM_DOWNLOAD_ROOT`` (default ``models/stt-gigaam``). Имя модели —
``GIGAAM_MODEL`` (default ``v3_e2e_rnnt`` — пунктуация + лучший WER среди
пунктуированных; другие: v3_rnnt — минимальный WER без пунктуации, v3_ctc,
v3_e2e_ctc, v2_ctc, ...).

Инференс — in-memory (без ffmpeg/файлов): PCM16 16 кГц mono → torch-тензор →
``model.forward`` + ``model._decode``. Работает на CPU (torch CPU-сборка):
~0.1 с на 3 с аудио; на CUDA (torch+cu) — быстрее.

LAZY LOADING: модель грузится при первом запросе (singleton + lock), как у
faster-whisper. Ошибка/молчание → ``STTResult(text="", ...)`` (не 500).
"""
from __future__ import annotations

import logging
import os
import threading

import numpy as np

from .providers import SAMPLE_RATE, STTResult, extract_pcm16

logger = logging.getLogger("voice.stt")

#: Известные имена моделей (gigaam 0.2.0). Короткие имена (ctc/rnnt/...) → v3_*.
MODEL_NAMES = (
    "v3_ctc", "v3_rnnt", "v3_e2e_ctc", "v3_e2e_rnnt", "v3_ssl",
    "v2_ctc", "v2_rnnt", "v2_ssl",
    "v1_ctc", "v1_rnnt", "v1_ssl",
    "multilingual_ctc", "multilingual_large_ctc",
    "ctc", "rnnt", "e2e_ctc", "e2e_rnnt", "ssl",
)

#: RNNT не возвращает per-token вероятности — фиксированный confidence
#: (модель детерминированная, на тишине не галлюцинирует; используется только
#: в conf-фильтре api, порог 0.5 — всегда проходит при непустом тексте).
_RNNT_CONFIDENCE = 0.9


class GigaAMSTT:
    """GigaAM (torch) — STT-провайдер с ленивой загрузкой модели."""

    name = "gigaam"

    def __init__(
        self,
        model_name: str = "",
        device: str = "",
        download_root: str = "",
    ) -> None:
        self._model_name = (model_name or os.getenv("GIGAAM_MODEL", "v3_e2e_rnnt")).strip()
        if self._model_name not in MODEL_NAMES:
            logger.warning(
                "GIGAAM_MODEL=%r неизвестен; использован default 'v3_e2e_rnnt'", model_name
            )
            self._model_name = "v3_e2e_rnnt"
        # Device: env GIGAAM_DEVICE (cpu|cuda); по умолчанию auto (torch сам выберет
        # cuda при наличии CUDA-сборки). STT_DEVICE НЕ наследуем — он для ctranslate2
        # (faster-whisper) и некорректен для torch CPU-сборки.
        self._device = (device or os.getenv("GIGAAM_DEVICE", "")).lower()
        if self._device == "cuda":
            try:
                import torch

                if not torch.cuda.is_available():
                    logger.warning("GIGAAM_DEVICE=cuda, но CUDA недоступна (torch CPU-сборка) -> cpu")
                    self._device = "cpu"
            except ImportError:
                pass
        self._download_root = download_root or os.getenv("GIGAAM_DOWNLOAD_ROOT") or None
        self._model = None
        self._lock = threading.Lock()

    # ------------------------------------------------------------------ lazy
    def _ensure_model(self):
        """Загружает модель один раз (singleton + lock); блокирует только на загрузке."""
        if self._model is None:
            with self._lock:
                if self._model is None:
                    import gigaam

                    logger.info(
                        "Загрузка STT-модели gigaam=%s device=%s root=%s",
                        self._model_name,
                        self._device or "auto",
                        self._download_root or "~/.cache/gigaam",
                    )
                    self._model = gigaam.load_model(
                        self._model_name,
                        device=self._device or None,
                        download_root=self._download_root,
                    )
        return self._model

    @property
    def loaded(self) -> bool:
        return self._model is not None

    # --------------------------------------------------------------- contract
    def transcribe(self, audio: bytes, sample_rate: int = SAMPLE_RATE) -> STTResult:
        """PCM16 mono (raw или WAV) → текст. Ошибки/молчание → text='' (не 500)."""
        import torch

        sr = sample_rate or SAMPLE_RATE
        duration = (len(audio) / 2 / sr) if audio else 0.0
        try:
            extracted = extract_pcm16(audio)
            if extracted is None or not extracted[0]:
                logger.warning("Пустое/неподдерживаемое аудио (%d байт) -> text=''", len(audio))
                return STTResult(text="", confidence=0.0, duration_s=round(duration, 3))
            pcm, wav_sr = extracted
            if len(pcm) % 2:
                pcm = pcm[:-1]
            sr_eff = wav_sr or sr
            samples = np.frombuffer(pcm, dtype=np.int16).astype(np.float32) / 32768.0
            if sr_eff != SAMPLE_RATE and len(samples) > 1:
                n = max(2, int(round(len(samples) * SAMPLE_RATE / sr_eff)))
                samples = np.interp(
                    np.linspace(0.0, len(samples) - 1, n), np.arange(len(samples)), samples
                )
                duration = n / SAMPLE_RATE
            model = self._ensure_model()

            wav = torch.from_numpy(samples).unsqueeze(0)  # float32 mono, 16 кГц (после ресемпла)
            wav = wav.to(model._device).to(model._dtype)
            length = torch.full([1], wav.shape[-1], device=model._device)
            encoded, encoded_len = model.forward(wav, length)
            text, _words = model._decode(encoded, encoded_len, length, word_timestamps=False)[0]
            text = (text or "").strip()
            if not text:
                return STTResult(text="", confidence=0.0, duration_s=round(duration, 3))

            confidence = self._confidence(model, encoded, encoded_len, text)
            return STTResult(
                text=text, confidence=round(confidence, 3), duration_s=round(duration, 3)
            )
        except Exception:
            logger.exception("STT-распознавание gigaam завершилось ошибкой -> text=''")
            return STTResult(text="", confidence=0.0, duration_s=round(duration, 3))

    @staticmethod
    def _confidence(model, encoded, encoded_len, text: str) -> float:
        """Уверенность: среднюю вероятность выданных токенов (CTC log-probs).

        RNNT (жадный, без log-prob в публичном API) — константа _RNNT_CONFIDENCE.
        """
        decoding = model.decoding
        if decoding.__class__.__name__ == "CTCGreedyDecoding":
            try:
                import torch

                with torch.inference_mode():
                    log_probs = model.head(encoder_output=encoded)
                    labels = log_probs.argmax(dim=-1)
                    blank = decoding.blank_id
                    skip = labels != blank
                    skip[:, 1:] &= labels[:, 1:] != labels[:, :-1]
                    time_idx = torch.arange(labels.shape[1], device=labels.device)[None, :]
                    skip &= time_idx < encoded_len[:, None]
                    idx = skip.nonzero(as_tuple=False)
                    if idx.numel() == 0:
                        return _RNNT_CONFIDENCE
                    token_logp = log_probs[idx[:, 0], idx[:, 1], labels[skip]]
                    return float(np.clip(np.exp(token_logp.cpu().numpy()).mean(), 0.0, 1.0))
            except Exception:
                logger.debug("CTC confidence не посчитан -> константа", exc_info=True)
        return _RNNT_CONFIDENCE

    # ------------------------------------------------------------------ health
    def health(self) -> dict:
        return {
            "provider": self.name,
            "model": self._model_name,
            "device": (
                str(self._model._device) if self._model is not None else (self._device or "auto")
            ),
            "loaded": self.loaded,
        }
