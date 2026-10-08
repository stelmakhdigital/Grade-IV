"""Замер VAD (ADR-002, поправка 2026-10-09): energy vs Silero (onnx).

(а) Ложные реплики: для каждой фикстуры (tests/fixtures/) прогон:
    (i)  EnergyVAD — константы как в /stt/stream (threshold 0.5, min_silence
         600, min_speech 250, energy_rms 0.005);
    (ii) реальный SileroVAD (get_vad_model) — те же пороги 600/250.
    Метрики: число speech-сегментов, суммарная длительность речи (мс).
(б) Latency конца: speech_honest.pcm + 1.5 с тишины; время от реального
    конца речи (3.0 с — конец фикстуры) до end-события:
    EnergyVAD с EndSilenceMS=900 (как Go-детектор batch-пути) vs
    SileroVAD (min_silence 600).

Запуск:  .venv/bin/python tests/measure_vad.py
Результат — таблица в stdout (для docs/test-results/).
"""
from __future__ import annotations

import sys
from pathlib import Path

import numpy as np

sys.path.insert(0, str(Path(__file__).parent.parent))

from app.stt_stream import (  # noqa: E402
    EnergyVAD,
    PREROLL_SAMPLES,
    SAMPLE_RATE,
    SileroVAD,
    StreamVAD,
    VAD_MIN_SPEECH_MS,
    VAD_MIN_SILENCE_MS,
    VAD_PRE_SILENCE_MS,
    VAD_THRESHOLD,
)

FIX = Path(__file__).parent / "fixtures"
TALK_END_MS = 3000  # «реальный конец речи» в speech_honest.pcm (3.0 с)


def load(name: str) -> np.ndarray:
    return np.fromfile(FIX / f"{name}.pcm", dtype=np.int16).astype(np.float32) / 32768


def run_stream(vad, audio: np.ndarray, preroll: int = PREROLL_SAMPLES):
    """StreamVAD-прогон (кадры 250 мс, как реальный поток) → события."""
    v = StreamVAD(vad, preroll_samples=preroll, pre_silence_ms=VAD_PRE_SILENCE_MS)
    events: list[tuple[str, int, int]] = []
    step = int(0.25 * SAMPLE_RATE)
    for i in range(0, len(audio), step):
        events.extend(v.feed(audio[i:i + step]))
    return events


def ms(a: int, b: int) -> float:
    return (b - a) * 1000 / SAMPLE_RATE


def measure_fixture(name: str, vad) -> tuple[int, float, int]:
    """Фикстура + 1.5 с тишины → (сегментов, сумма речи мс, pre_end-ов)."""
    audio = np.concatenate([load(name), np.zeros(int(1.5 * SAMPLE_RATE), dtype=np.float32)])
    events = run_stream(vad.clone(), audio)
    segs = [e for e in events if e[0] == "end"]
    return len(segs), sum(ms(a, b) for _, a, b in segs), sum(1 for e in events if e[0] == "pre_end")


def main() -> None:
    energy = EnergyVAD()  # константы /stt/stream: 600/250, rms 0.005
    try:
        silero = SileroVAD()
    except Exception as exc:
        print(f"SILERO UNAVAILABLE: {exc}")
        return

    print("## (а) Ложные реплики (фикстура 3 с + 1.5 с тишины)")
    print()
    print("| фикстура | energy: сегм. | energy: речь, мс | silero: сегм. | silero: речь, мс |")
    print("|---|---|---|---|---|")
    for name in ("noise", "breathing", "silence", "speech_quiet", "speech_honest"):
        e_segs, e_ms, _ = measure_fixture(name, energy)
        s_segs, s_ms, _ = measure_fixture(name, silero)
        print(f"| {name} | {e_segs} | {e_ms:.0f} | {s_segs} | {s_ms:.0f} |")

    print()
    print("## (б) Latency конца (конец речи → end-событие), speech_honest + 1.5 с тишины")
    print()
    print("end-событие генерируется в аудио-координатах ~конец_речи + порог_тишины")
    print("(+ ≤32 мс — гранулярность окна VAD): latency ≈ порог тишины.")
    print()
    print("| VAD | порог тишины, мс | конец реплики, мс | end-событие (оценка), мс | latency, мс |")
    print("|---|---|---|---|---|")
    for label, vad, silence_ms in (
        ("EnergyVAD (Go-batch)", EnergyVAD(min_silence_ms=900), 900),
        ("SileroVAD (voice)", SileroVAD(), VAD_MIN_SILENCE_MS),
    ):
        audio = np.concatenate([load("speech_honest"), np.zeros(int(1.5 * SAMPLE_RATE), dtype=np.float32)])
        events = run_stream(vad.clone(), audio)
        ends = [e for e in events if e[0] == "end"]
        if not ends:
            print(f"| {label} | {silence_ms} | — (нет end) | — | — |")
            continue
        # конец реплики = последний end-сегмент; событие — в момент,
        # когда тишина после silence_since набрала порог.
        _k, a, b = ends[-1]
        end_at = b * 1000 / SAMPLE_RATE + silence_ms  # + ≤32 мс (окно VAD)
        print(f"| {label} | {silence_ms} | {b * 1000 / SAMPLE_RATE:.0f} | ~{end_at} | ~{end_at - TALK_END_MS} |")
    print()
    print(f"(«конец речи» = {TALK_END_MS} мс — конец фикстуры; pre-roll не влияет на позицию")
    print(" конца; min_speech = "
          f"{VAD_MIN_SPEECH_MS} мс, порог prob = {VAD_THRESHOLD}; окно VAD 32 мс)")


if __name__ == "__main__":
    main()
