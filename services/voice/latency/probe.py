#!/usr/bin/env python3
"""Latency-пробер голосового контура «Грейд» (Фаза 4, TEST_PLAN §3).

Цепочка замеров на живой сессии (WS api):
    send_start ──PCM 250мс чанки──> VAD ──/stt──> LLM ──/tts──> TTS-кадр
    t0 (первый чанк)
    t_stt (событие user_utterance — STT-результат пришёл)
    t_llm (первый ai_text/ai_utterance — LLM-ответ)
    t_tts (первый бинарный TTS-кадр — синтез начался)

Использование:
  1) api (LLM_MOCK=1, VOICE_URL=http://127.0.0.1:8100, VAD_END_SILENCE_MS=900)
  2) voice: .venv/bin/uvicorn app.main:app --port 8100
     (VOICE_STT_PROVIDER=faster-whisper STT_MODEL=tiny; silero)
  3) python3 latency/probe.py [--api http://127.0.0.1:8900] [--runs N]

Синтез эталона — через voice /api/v1/tts (Silero v5, 24 kHz mono s16le).
Отчёт: stdout + JSON в docs/test-results/voice-latency-{ts}.json.
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

import websockets

SPEAKER_RATE = 16000  # Hz, PCM16 mono — контракт WS api (ADR-001)
CHUNK_MS = 250
CHUNK_SAMPLES = SPEAKER_RATE * CHUNK_MS // 1000
TAIL_SILENCE_MS = 900  # хвост тишины для VAD-срабатывания
RECV_TIMEOUT = 60.0


def synth_pcm(tts_base: str, text: str) -> bytes:
    req = urllib.request.Request(
        f"{tts_base}/api/v1/tts",
        data=json.dumps({"text": text, "speaker": "eugene"}).encode(),
        headers={"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(req, timeout=120) as r:
        return r.read()


def resample_24k_to_16k(pcm: bytes) -> bytes:
    """api-контур принимает PCM16 16 kHz mono (ADR-001); Silero TTS отдаёт
    24 kHz — линейный ресемплинг 2:3 (точность для синтетической речи достаточна)."""
    import array
    samples = array.array('h')
    samples.frombytes(pcm[: len(pcm) // 2 * 2])
    n = len(samples)
    out = array.array('h')
    for i in range(n * 2 // 3):
        pos = i * 3 / 2
        k = int(pos)
        frac = pos - k
        a = samples[k]
        b = samples[k + 1] if k + 1 < n else a
        out.append(int(a + (b - a) * frac))
    return out.tobytes()


def lev_ratio(a: str, b: str) -> float:
    a, b = a.lower().strip(), b.lower().strip()
    for ch in ".,!?;:—-":
        a, b = a.replace(ch, ""), b.replace(ch, "")
    if not a and not b:
        return 1.0
    dp = list(range(len(b) + 1))
    for i, ca in enumerate(a, 1):
        prev, dp[0] = dp[0], i
        for j, cb in enumerate(b, 1):
            prev, dp[j] = dp[j], min(dp[j] + 1, dp[j - 1] + 1, prev + (ca != cb))
    return 1 - dp[-1] / max(len(a), len(b))


class Run:
    __slots__ = ("idx", "t_stt", "t_llm", "t_tts", "stt_text", "ok")

    def __init__(self, idx: int):
        self.idx = idx
        self.t_stt: float | None = None
        self.t_llm: float | None = None
        self.t_tts: float | None = None
        self.stt_text = ""
        self.ok = False


async def one_run(ws_url: str, pcm: bytes, idx: int) -> Run:
    run = Run(idx)
    t0 = time.perf_counter()
    async with websockets.connect(ws_url, max_size=4 * 1024 * 1024) as ws:
        # дождаться первой стадии (stage)
        while True:
            msg = await asyncio.wait_for(ws.recv(), timeout=RECV_TIMEOUT)
            if isinstance(msg, (bytes, bytearray)):
                continue
            d = json.loads(msg)
            if d.get("type") == "stage" and (d.get("task") or d.get("name") == "voice"):
                break
            if d.get("type") == "error":
                raise RuntimeError(f"ws error: {d}")
        # turn-taking: TTS-кадры api шлёт burst'ом (streamAIAudio без пауз) —
        # ждём конец потока (flag 0x0002 в заголовке 4 байта), затем запас 0.5 с.
        import struct as _st
        tts_done = False
        while not tts_done:
            try:
                msg = await asyncio.wait_for(ws.recv(), timeout=RECV_TIMEOUT)
            except asyncio.TimeoutError:
                break  # TTS не будет (mock/ошибка) — не блокируемся
            if isinstance(msg, (bytes, bytearray)) and len(msg) >= 4:
                _seq, flags = _st.unpack_from('<HH', msg[:4])
                if flags & 0x0001:
                    tts_done = True
            else:
                continue  # текст (ai_text и т.п.) — продолжаем ждать конец TTS
        await asyncio.sleep(0.5)
        chunks = [pcm[i : i + CHUNK_SAMPLES * 2] for i in range(0, len(pcm), CHUNK_SAMPLES * 2)]
        tail = b"\x00" * (CHUNK_SAMPLES * 2)
        # микрофон не перестаёт слать кадры: речь + 12 кадров тишины (3 с) —
        # VAD ждёт EndSilenceMS тишины (900 мс) внутри непрерывного потока.
        for i, ch in enumerate(chunks + [tail] * 12):
            await ws.send(ch)
            if i == 0:
                t0 = time.perf_counter()
            await asyncio.sleep(CHUNK_MS / 1000)
        # приём до первого TTS-кадра (и пока не всё поймано)
        deadline = time.perf_counter() + RECV_TIMEOUT
        while time.perf_counter() < deadline and not (run.t_tts and run.t_stt):
            try:
                msg = await asyncio.wait_for(ws.recv(), timeout=2.0)
            except asyncio.TimeoutError:
                if run.t_tts and run.t_stt and run.t_llm:
                    break
                continue
            if isinstance(msg, (bytes, bytearray)):
                if run.t_tts is None:
                    run.t_tts = time.perf_counter() - t0
                continue
            d = json.loads(msg)
            typ = d.get("type")
            if typ == "transcript" and run.t_stt is None:
                run.t_stt = time.perf_counter() - t0
                run.stt_text = str(d.get("text") or "")
            elif typ in ("ai_text", "ai_utterance") and run.t_llm is None:
                run.t_llm = time.perf_counter() - t0
        run.ok = run.t_stt is not None
    return run


def pct(vals, p):
    if not vals:
        return None
    vals = sorted(vals)
    k = (len(vals) - 1) * p / 100
    f, c = int(k), min(int(k) + 1, len(vals) - 1)
    return vals[f] + (vals[c] - vals[f]) * (k - f)


async def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--api", default="http://127.0.0.1:8900")
    ap.add_argument("--tts", default="http://127.0.0.1:8100")
    ap.add_argument("--runs", type=int, default=20)
    ap.add_argument("--out", default="")
    args = ap.parse_args()

    etalon = json.loads(Path(__file__).with_name("etalon20.json").read_text())
    texts = etalon[: args.runs]

    # регистрация + сессия
    def http_json(method, url, body=None, headers=None):
        h = dict(headers or {})
        data = None
        if body is not None:
            data = json.dumps(body).encode()
            h["Content-Type"] = "application/json"
        req = urllib.request.Request(url, data=data, headers=h, method=method)
        with urllib.request.urlopen(req, timeout=30) as r:
            return json.loads(r.read() or b"null")

    reg = http_json("POST", f"{args.api}/api/v1/auth/register",
                    {"email": f"probe-{int(time.time())}@example.com", "password": "password1"})
    auth = {"Authorization": f"Bearer {reg['token']}"}
    sess = http_json("POST", f"{args.api}/api/v1/sessions",
                     {"grade": "middle", "stack": "go"}, auth)
    sid = sess["id"]
    ws_url = args.api.replace("http://", "ws://").replace("https://", "wss://")
    ws_url += f"/ws/session/{sid}?token={reg['token']}"

    print(f"api={args.api} tts={args.tts} session={sid} runs={len(texts)}", flush=True)
    runs: list[Run] = []
    for i, text in enumerate(texts):
        pcm = resample_24k_to_16k(synth_pcm(args.tts, text))
        dur = len(pcm) / 2 / SPEAKER_RATE
        r = await one_run(ws_url, pcm, i)
        # завершить сессию перед следующим разом (одна активная на пользователя)
        try:
            http_json("POST", f"{args.api}/api/v1/sessions/{sid}/finish", None, auth)
        except Exception:
            pass
        line = f"[{i + 1}/{len(texts)}] dur={dur:.1f}s"
        if r.ok:
            sim = lev_ratio(text, r.stt_text)
            line += (f" stt={r.t_stt:.2f}s llm={('%.2f' % r.t_llm) if r.t_llm else '—'}"
                     f" tts={('%.2f' % r.t_tts) if r.t_tts else '—'} sim={sim:.2f}")
            line += f" | «{r.stt_text[:60]}»"
        else:
            line += " FAIL (нет STT-результата)"
        print(line, flush=True)
        runs.append(r)
        # новая сессия на следующий прогон
        if i + 1 < len(texts):
            sess = http_json("POST", f"{args.api}/api/v1/sessions",
                             {"grade": "middle", "stack": "go"}, auth)
            sid = sess["id"]
            ws_url = (args.api.replace("http://", "ws://").replace("https://", "wss://")
                      + f"/ws/session/{sid}?token={reg['token']}")

    ok_runs = [r for r in runs if r.ok]
    rep = {
        "ts": time.strftime("%Y-%m-%dT%H:%M:%S"),
        "api": args.api, "tts": args.tts, "runs": len(texts),
        "completed": len(ok_runs),
        "stt_s": {"p50": pct([r.t_stt for r in ok_runs], 50),
                  "p95": pct([r.t_stt for r in ok_runs], 95)},
        "tts_s": {"p50": pct([r.t_tts for r in ok_runs if r.t_tts], 50),
                  "p95": pct([r.t_tts for r in ok_runs if r.t_tts], 95)},
        "stt_similarity": {"mean": statistics.mean(
            [lev_ratio(t, r.stt_text) for t, r in zip(texts, runs) if r.ok]) if ok_runs else None},
        "detail": [{"idx": r.idx, "stt_s": r.t_stt, "llm_s": r.t_llm, "tts_s": r.t_tts,
                    "text": r.stt_text} for r in runs],
    }
    for k in ("stt_s", "tts_s"):
        for p in rep[k]:
            if rep[k][p] is not None:
                rep[k][p] = round(rep[k][p], 3)
    if rep["stt_similarity"]["mean"] is not None:
        rep["stt_similarity"]["mean"] = round(rep["stt_similarity"]["mean"], 3)
    print(json.dumps(rep, ensure_ascii=False, indent=2))
    if args.out:
        Path(args.out).write_text(json.dumps(rep, ensure_ascii=False, indent=2))
        print(f"отчёт: {args.out}")


if __name__ == "__main__":
    asyncio.run(main())
