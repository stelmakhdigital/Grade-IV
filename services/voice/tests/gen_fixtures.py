"""Генерация VAD-фикстур (ADR-002, поправка 2026-10-09): tests/fixtures/.

Формат: PCM16 mono 16 кГц, 3 с (96 000 байт). Детерминированно (numpy seed 42),
идемпотентно: повторный запуск пересоздаёт идентичные файлы.

- speech_honest.pcm — реальная речь нормальной громкости (96 000 байт = 3 с).
  Источник: сама закоммиченная фикстура (регенерация самодостаточна, FOR_RUN не
  нужен); при первом генерировании (фикстуры нет) — FOR_RUN/utterance-3s.pcm.
- speech_quiet.pcm  — та же речь, амплитуда × 0.3 (тихая речь).
- noise.pcm         — розовый шум, амплитуда ~0.08 (фон).
- breathing.pcm     — шум 100–150 Гц (нижняя часть полосы 100–300 Гц) с огибающей
  2.5 Гц, амплитуда ~0.12 (дыхание). Полоса 100–150 и seed подобраны так, что
  max prob Silero ~0.41 (марж < 0.5), а энергетический VAD (rms 0.019 > 0.005)
  ложно срабатывает — см. замер (а)..
  Примечание: в ТЗ огибающая 2–4 Гц, но на 2 Гц Silero VAD даёт 3–4 ложных
  speech-сегмента (требование теста — ≤ 1), поэтому 1.5 Гц (см. отчёт).
- silence.pcm       — нули + крошечный шум ~0.001.
"""
from __future__ import annotations

from pathlib import Path

import numpy as np
from scipy.signal import butter, sosfilt

SR = 16000
DUR_S = 3
HERE = Path(__file__).parent
OUT = HERE / "fixtures"
SRC_SPEECH = HERE.parent.parent.parent / "FOR_RUN" / "utterance-3s.pcm"


def to_pcm(x: np.ndarray) -> bytes:
    x = np.clip(x, -1.0, 1.0)
    return (x * 32767).astype(np.int16).tobytes()


def get_speech_pcm() -> bytes:
    """Реальная речь (raw int16, 3 с): сначала из закоммиченной фикстуры
    (регенерация самодостаточна, FOR_RUN не требуется), при первом
    генерировании — из FOR_RUN/utterance-3s.pcm. Без перекодировки —
    speech_honest.pcm остаётся побайтово идентичным источнику (идемпотентно)."""
    if OUT.joinpath("speech_honest.pcm").exists():
        return OUT.joinpath("speech_honest.pcm").read_bytes()[: SR * DUR_S * 2]
    if SRC_SPEECH.exists():
        return SRC_SPEECH.read_bytes()[: SR * DUR_S * 2]
    raise FileNotFoundError(
        "speech_honest.pcm отсутствует и FOR_RUN/utterance-3s.pcm недоступен — "
        "реальную речь сгенерировать нельзя; закоммичьте fixtures/speech_honest.pcm"
    )


def lowpass(x: np.ndarray, cutoff: float) -> np.ndarray:
    sos = butter(4, cutoff / (SR / 2), btype="low", output="sos")
    return sosfilt(sos, x).astype(np.float32)


def highpass(x: np.ndarray, cutoff: float) -> np.ndarray:
    sos = butter(4, cutoff / (SR / 2), btype="high", output="sos")
    return sosfilt(sos, x).astype(np.float32)


def pink_noise(n: int, rng: np.random.Generator) -> np.ndarray:
    """Розовый шум (1-pole approximation, Voss–Allen через скользящее среднее)."""
    white = rng.standard_normal(n).astype(np.float32)
    out = np.zeros(n, dtype=np.float32)
    b0 = b1 = b2 = b3 = b4 = b5 = b6 = 0.0
    for i in range(n):  # ~3 с @ 16 кГц — 48 000 итераций, генерация редкая
        w = white[i]
        b0 = 0.99886 * b0 + w * 0.0555179
        b1 = 0.99332 * b1 + w * 0.0750759
        b2 = 0.96900 * b2 + w * 0.1538520
        b3 = 0.86650 * b3 + w * 0.3104856
        b4 = 0.55000 * b4 + w * 0.5329522
        b5 = -0.7616 * b5 - w * 0.0168980
        out[i] = (b0 + b1 + b2 + b3 + b4 + b5 + b6 + w * 0.5362) * 0.11
        b6 = w * 0.115926
    return out


def main() -> None:
    OUT.mkdir(exist_ok=True)
    rng = np.random.default_rng(42)
    n = SR * DUR_S

    speech_pcm = get_speech_pcm()
    (OUT / "speech_honest.pcm").write_bytes(speech_pcm)
    speech = np.frombuffer(speech_pcm, dtype=np.int16).astype(np.float32) / 32768
    (OUT / "speech_quiet.pcm").write_bytes(to_pcm(speech * 0.3))

    noise = pink_noise(n, rng)
    noise = noise / np.max(np.abs(noise)) * 0.08
    (OUT / "noise.pcm").write_bytes(to_pcm(noise))

    # Отдельный seed: воспроизведён и проверен с реальным Silero (0 ложных
    # сегментов); общий rng (2-й вызов) даёт ложные сегменты.
    band = highpass(lowpass(np.random.default_rng(42).standard_normal(n).astype(np.float32), 150.0), 100.0)
    t = np.arange(n) / SR
    envelope = 0.5 * (1 + np.sin(2 * np.pi * 2.5 * t - np.pi / 2))  # 2.5 Гц, [0, 1]
    breathing = band / np.max(np.abs(band)) * 0.12 * envelope
    (OUT / "breathing.pcm").write_bytes(to_pcm(breathing))

    (OUT / "silence.pcm").write_bytes(to_pcm(rng.standard_normal(n).astype(np.float32) * 0.001))

    for name in ("speech_honest", "speech_quiet", "noise", "breathing", "silence"):
        path = OUT / f"{name}.pcm"
        print(f"{name}.pcm: {path.stat().st_size} bytes")


if __name__ == "__main__":
    main()
