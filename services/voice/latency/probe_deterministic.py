#!/usr/bin/env python3
"""Deterministic latency-пробер голосового контура «Грейд» (T-20261009001516).

Цель: A/B «до #39/#40» (worktree 4a6e048) vs «после» (HEAD) на ФИКСИРОВАННОМ
LLM (api с LLM_MOCK=1 + LLM_MOCK_RESPONSE + LLM_MOCK_TOKENS_PER_S) — LLM-фактор
вынесен из измерения. Меряется пайплайн: VAD → STT → dispatch → TTS → первый кадр.

Отсчёт — КОНЕЦ речи кандидата (конец аудио фиксированной ~3-с реплики), а не
начало. Реплика синтезируется один раз через voice /api/v1/tts (Silero v5,
детерминированно) и переиспользуется во всех прогонах.

Метрики:
  first_tts — конец речи → первый TTS-кадр (SLO p95 < 4 с, «пайплайн-часть»);
  turn_end  — конец речи → end-кадр (конец потока ответа ИИ);
  gaps      — межфразовые разрывы: последовательности тише-кадров (RMS) между
              аудио-сегментами, мс (пауза после «.»/«?/!»).

Таймлайн одного прогона:
  connect → stage=voice → [приветствие ИИ: TTS-стрим, ждём end-флаг, дренируем]
  → t0, шлём PCM-кадры реплики (реальное время) + 3 с тишины
  → end_of_speech = t0 + dur → ждём ответ ИИ: первый кадр (t_tts), end-кадр (t_end)

Использование:
  python3 probe_deterministic.py --api http://127.0.0.1:8910 \
      --tts http://127.0.0.1:8100 --runs 8 --out report.json
"""
from __future__ import annotations

import argparse
import asyncio
import json
import statistics
import struct
import time
import urllib.request
from pathlib import Path

import numpy as np
import websockets

RATE = 16000
CHUNK_MS = 250
CHUNK_SAMPLES = RATE * CHUNK_MS // 1000
CHUNK_BYTES = CHUNK_SAMPLES * 2
TAIL_S = 1.0          # хвост тишины после реплики (VAD 600 мс; в реальном времени)
RECV_TIMEOUT = 60.0
END_FLAG = 0x0001     # flags: последний кадр потока (ADR-001)
SILENCE_RMS = 0.01    # порог «тише-кадр» для анализа разрывов


def http_json(method, url, body=None, headers=None):
    h = dict(headers or {})
    data = None
    if body is not None:
        data = json.dumps(body).encode()
        h["Content-Type"] = "application/json"
    req = urllib.request.Request(url, data=data, headers=h, method=method)
    with urllib.request.urlopen(req, timeout=60) as r:
        return json.loads(r.read() or b"null")


def synth_pcm(tts_base: str, text: str, speaker: str = "eugene") -> bytes:
    """Реплика кандидата: voice /api/v1/tts (Silero v5, 24 кГц) → 16 кГц."""
    req = urllib.request.Request(
        f"{tts_base}/api/v1/tts",
        data=json.dumps({"text": text, "speaker": speaker}).encode(),
        headers={"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(req, timeout=120) as r:
        pcm24 = r.read()
    import array
    s = array.array("h")
    s.frombytes(pcm24[: len(pcm24) // 2 * 2])
    n = len(s)
    out = array.array("h")
    for i in range(n * 2 // 3):  # 24→16 кГц (2:3), линейная интерполяция
        pos = i * 3 / 2
        k = int(pos)
        frac = pos - k
        a = s[k]
        b = s[k + 1] if k + 1 < n else a
        out.append(int(a + (b - a) * frac))
    return out.tobytes()


def frame_rms(pcm: bytes) -> float:
    if len(pcm) < 2:
        return 0.0
    x = np.frombuffer(pcm[: len(pcm) // 2 * 2], dtype=np.int16).astype(np.float32) / 32768
    return float(np.sqrt(np.mean(x ** 2))) if x.size else 0.0


def pct(vals, p):
    if not vals:
        return None
    vals = sorted(vals)
    k = (len(vals) - 1) * p / 100
    f, c = int(k), min(int(k) + 1, len(vals) - 1)
    return vals[f] + (vals[c] - vals[f]) * (k - f)


class Run:
    def __init__(self, idx):
        self.idx = idx
        self.end_of_speech = 0.0   # t0 + dur (abs)
        self.t_stt = None          # end_of_speech → transcript (VAD+STT), с
        self.first_tts = None      # end_of_speech → первый кадр, с
        self.turn_end = None       # end_of_speech → end-кадр, с
        self.stt_text = ""
        self.gaps_ms = []          # межфразовые разрывы, мс
        self.ok = False


async def wait_greeting_done(ws) -> None:
    """Приветствие ИИ (TTS-стрим) — ждём end-флаг (0x01), дренируем кадры."""
    while True:
        msg = await asyncio.wait_for(ws.recv(), timeout=RECV_TIMEOUT)
        if isinstance(msg, (bytes, bytearray)):
            if len(msg) >= 4:
                _seq, flags = struct.unpack_from("<HH", msg[:4])
                if flags & END_FLAG:
                    return
        else:
            d = json.loads(msg)
            if d.get("type") == "error":
                raise RuntimeError(f"ws error: {d}")


async def one_run(ws_url: str, pcm: bytes, dur: float, idx: int) -> Run:
    run = Run(idx)
    async with websockets.connect(ws_url, max_size=8 * 1024 * 1024) as ws:
        # stage=voice
        while True:
            msg = await asyncio.wait_for(ws.recv(), timeout=RECV_TIMEOUT)
            if isinstance(msg, (bytes, bytearray)):
                continue
            d = json.loads(msg)
            if d.get("type") == "stage":
                break
            if d.get("type") == "error":
                raise RuntimeError(f"ws error: {d}")
        # дренируем приветствие (ИИ говорит → занят; кандидат ждёт)
        await wait_greeting_done(ws)
        await asyncio.sleep(0.6)  # busy-release после end-кадра приветствия

        # send реплики + хвост тишины — в РЕАЛЬНОМ времени (каждый кадр 250 мс):
        # тишина приходит после конца аудио → VAD-хвост 600 мс отсчитывается от
        # истинного конца речи (end_of_speech = t0 + dur).
        chunks = [pcm[i:i + CHUNK_BYTES] for i in range(0, len(pcm), CHUNK_BYTES)]
        tail = b"\x00" * CHUNK_BYTES
        tail_n = int(TAIL_S * 1000 / CHUNK_MS)
        all_chunks = chunks + [tail] * tail_n
        t0 = time.perf_counter()
        run.end_of_speech = t0 + dur  # конец аудио реплики
        for i, ch in enumerate(all_chunks):
            await ws.send(ch)
            if i < len(all_chunks) - 1:
                await asyncio.sleep(CHUNK_MS / 1000)

        # приём ответа: первый кадр, end-кадр, RMS-кадров (для разрывов)
        deadline = time.perf_counter() + RECV_TIMEOUT
        frames = []  # (rel_t, rms)
        got_end = False
        while time.perf_counter() < deadline and not got_end:
            try:
                msg = await asyncio.wait_for(ws.recv(), timeout=2.0)
            except asyncio.TimeoutError:
                continue
            rel = time.perf_counter()
            if isinstance(msg, (bytes, bytearray)):
                if run.first_tts is None:
                    run.first_tts = rel - run.end_of_speech
                frames.append((rel, frame_rms(msg[4:] if len(msg) >= 4 else msg)))
                if len(msg) >= 4:
                    _seq, flags = struct.unpack_from("<HH", msg[:4])
                    if flags & END_FLAG:
                        run.turn_end = rel - run.end_of_speech
                        got_end = True
            else:
                d = json.loads(msg)
                if d.get("type") == "transcript" and run.t_stt is None:
                    run.t_stt = rel - run.end_of_speech
                    run.stt_text = str(d.get("text") or "")
        run.ok = run.first_tts is not None

        # межфразовые разрывы: последовательности тише-кадров (RMS<SILENCE_RMS)
        # между аудио-сегментами; каждый тише-кадр = 250 мс.
        audio = [r >= SILENCE_RMS for _, r in frames]
        i = 0
        while i < len(audio):
            if not audio[i]:
                j = i
                while j < len(audio) and not audio[j]:
                    j += 1
                # тише-бег между аудио (не в самом начале/конце)
                if 0 < i and j < len(audio):
                    run.gaps_ms.append((j - i) * CHUNK_MS)
                i = j
            else:
                i += 1
    return run


async def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--api", default="http://127.0.0.1:8910")
    ap.add_argument("--tts", default="http://127.0.0.1:8100")
    ap.add_argument("--runs", type=int, default=8)
    ap.add_argument("--text", default="Расскажите о вашем подходе к проектированию микросервисов.")
    ap.add_argument("--speaker", default="eugene")
    ap.add_argument("--out", default="")
    args = ap.parse_args()

    pcm = synth_pcm(args.tts, args.text, args.speaker)
    dur = len(pcm) / 2 / RATE
    print(f"реплика: dur={dur:.2f}s text=«{args.text}»", flush=True)

    reg = http_json("POST", f"{args.api}/api/v1/auth/register",
                    {"email": f"det-{int(time.time())}@example.com", "password": "password1"})
    auth = {"Authorization": f"Bearer {reg['token']}"}
    base_ws = args.api.replace("http://", "ws://").replace("https://", "wss://")

    runs: list[Run] = []
    for i in range(args.runs):
        sess = http_json("POST", f"{args.api}/api/v1/sessions", {"grade": "middle", "stack": "go"}, auth)
        sid = sess["id"]
        ws_url = f"{base_ws}/ws/session/{sid}?token={reg['token']}"
        try:
            r = await one_run(ws_url, pcm, dur, i)
        except Exception as e:
            print(f"[{i+1}/{args.runs}] ERROR {e}", flush=True)
            r = Run(i)
        finally:
            try:
                http_json("POST", f"{args.api}/api/v1/sessions/{sid}/finish", None, auth)
            except Exception:
                pass
        if r.ok:
            stt = f"{r.t_stt:.2f}" if r.t_stt is not None else "—"
            llm_tts = f"{r.first_tts - r.t_stt:.2f}" if (r.t_stt is not None and r.first_tts is not None) else "—"
            gaps = f" gaps={r.gaps_ms}" if r.gaps_ms else ""
            print(f"[{i+1}/{args.runs}] first_tts={r.first_tts:.2f}s (stt={stt} + llm/tts={llm_tts}) "
                  f"turn_end={('%.2f' % r.turn_end) if r.turn_end else '—'}s{gaps} | «{r.stt_text[:40]}»", flush=True)
        else:
            print(f"[{i+1}/{args.runs}] FAIL (нет TTS-кадра)", flush=True)
        runs.append(r)

    ok = [r for r in runs if r.ok]
    ft = [r.first_tts for r in ok if r.first_tts is not None]
    te = [r.turn_end for r in ok if r.turn_end is not None]
    rep = {
        "ts": time.strftime("%Y-%m-%dT%H:%M:%S"),
        "api": args.api, "tts": args.tts, "runs": args.runs, "completed": len(ok),
        "candidate": {"text": args.text, "dur_s": round(dur, 3), "speaker": args.speaker},
        "first_tts_s": {"p50": pct(ft, 50), "p95": pct(ft, 95), "mean": statistics.mean(ft) if ft else None,
                        "all": [round(x, 3) for x in ft]},
        "stt_s": {"p50": pct([r.t_stt for r in ok if r.t_stt is not None], 50),
                  "all": [round(x, 3) for x in (r.t_stt for r in ok if r.t_stt is not None)]},
        "turn_end_s": {"p50": pct(te, 50), "p95": pct(te, 95), "mean": statistics.mean(te) if te else None,
                       "all": [round(x, 3) for x in te]},
        "gaps_ms": {"per_run": [r.gaps_ms for r in ok]},
    }
    for k in ("first_tts_s", "turn_end_s", "stt_s"):
        for p in ("p50", "p95", "mean"):
            if p in rep[k] and rep[k][p] is not None:
                rep[k][p] = round(rep[k][p], 3)
    print(json.dumps(rep, ensure_ascii=False, indent=2))
    if args.out:
        Path(args.out).write_text(json.dumps(rep, ensure_ascii=False, indent=2))
        print(f"отчёт: {args.out}")


if __name__ == "__main__":
    asyncio.run(main())
