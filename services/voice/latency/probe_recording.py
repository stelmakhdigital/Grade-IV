"""Live-проверка режима записи (FR-S8, T-20261009164629): запись 60-70 с.

Сценарий (реальный голос — Silero TTS voice-сервиса, реальный STT/LLM):
1. register → create session → WS;
2. recording on (как «включил микрофон»);
3. стриминг ~60-70 с речи (8 фраз, синтез /api/v1/tts) с паузами ~1.5 с
   в реальном времени (чанки 250 мс);
4. стt-сегменты (final по тишине) → события stt_segment, AI-хода НЕ ждём;
5. recording off + utterance (склеенный текст сегментов — как клиент);
6. ждём ОДИН ход: transcript(user) + ai_text (+ TTS-кадры) — ответ ИИ.
Выход: JSON-резюме (сегменты, speech_ms, таймлайн) → docs/test-results.

Запуск: services/voice/.venv/bin/python services/voice/latency/probe_recording.py \
  [--api http://localhost:8000] [--voice http://localhost:8100] [--out /tmp/rec-probe.json]
"""
from __future__ import annotations

import argparse
import asyncio
import json
import time
import urllib.request

import websockets

PHRASES = [
    "Здравствуйте! Меня зовут Анна, я backend-разработчик с опытом четыре года.",
    "Основной стек — Go: микросервисы, gRPC, PostgreSQL и Kafka.",
    "В последнем проекте я руководила командой из трёх человек и отвечала за производительность API.",
    "Сложность была в том, что p95 запросов росло до трёх секунд под нагрузкой.",
    "Мы добавили кэширование, пул соединений и асинхронную обработку, и p95 упало до четырёхсот миллисекунд.",
    "Также я внедрила мониторинг и алерты, которые помогли быстро находить деградации.",
    "Из инструментов — Docker, Kubernetes, Grafana и, конечно, Go testing с таблицами тестов.",
    "Из принципов разработки я придерживаюсь простоты: сначала читаемый код, потом оптимизация по метрикам.",
    "В командах я обычно беру на себя код-ревью и менторство джуниоров, это занимает примерно треть времени.",
    "Спасибо, готова ответить на вопросы по опыту или разобрать задачу.",
]
PAUSE_S = 1.5
CHUNK_S = 0.25


def http_json(method: str, url: str, body=None, headers=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    for k, v in (headers or {}).items():
        req.add_header(k, v)
    with urllib.request.urlopen(req, timeout=120) as r:
        return json.loads(r.read())


def tts_pcm(url: str, text: str) -> bytes:
    req = urllib.request.Request(
        url + "/api/v1/tts", data=json.dumps({"text": text}).encode(),
        method="POST", headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=120) as r:
        return r.read()


async def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--api", default="http://localhost:8000")
    ap.add_argument("--voice", default="http://localhost:8100")
    ap.add_argument("--out", default="/tmp/rec-probe.json")
    args = ap.parse_args()

    # 1) Токен + сессия.
    reg = http_json("POST", f"{args.api}/api/v1/auth/register",
                    {"email": f"rec-probe-{int(time.time())}@example.com", "password": "password1"})
    auth = {"Authorization": f"Bearer {reg['token']}"}
    sess = http_json("POST", f"{args.api}/api/v1/sessions",
                     {"grade": "middle", "stack": "go", "duration_limit_s": 1200}, auth)
    sid = sess["id"]

    # 2) Синтез фраз (реальный голос — Silero TTS voice-сервиса).
    pcms = [tts_pcm(args.voice, p) for p in PHRASES]
    total_audio_s = sum(len(p) for p in pcms) / 32000 + PAUSE_S * (len(pcms) - 1)
    print(f"аудио: {len(pcms)} фраз, {total_audio_s:.1f} с (паузы {PAUSE_S} с)")

    ws_url = args.api.replace("http://", "ws://").replace("https://", "wss://")
    ws_url += f"/ws/session/{sid}?token={reg['token']}"

    timeline: list[dict] = []
    segments: list[str] = []
    total_speech_ms = 0
    ai_turns = 0
    user_transcripts = 0

    async with websockets.connect(ws_url, max_size=1 << 24) as ws:
        def note(kind: str, **kw) -> None:
            e = {"t": round(time.time() - T0, 2), "kind": kind, **kw}
            timeline.append(e)
            print(json.dumps(e, ensure_ascii=False))

        T0 = time.time()

        # 3) recording on.
        await ws.send(json.dumps({"type": "ui", "name": "recording", "payload": {"on": True}}))
        note("recording_on")

        async def reader() -> None:
            nonlocal ai_turns, user_transcripts, total_speech_ms
            while True:
                try:
                    msg = await asyncio.wait_for(ws.recv(), timeout=180)
                except (TimeoutError, asyncio.TimeoutError):
                    return
                if isinstance(msg, bytes):
                    note("tts_frame", bytes=len(msg))
                    continue
                m = json.loads(msg)
                t = m.get("type")
                if t == "stt_segment":
                    segments.append(m["text"])
                    total_speech_ms = m.get("total_speech_ms", total_speech_ms)
                    note("stt_segment", text=m["text"][:80], speech_ms=m.get("speech_ms"),
                         total_speech_ms=m.get("total_speech_ms"))
                elif t == "transcript":
                    if m.get("who") == "user":
                        user_transcripts += 1
                        note("transcript_user", text=m.get("text", "")[:120])
                    else:
                        note("transcript_ai", text=m.get("text", "")[:120])
                elif t == "ai_text":
                    note("ai_text", text=m.get("text", "")[:120])
                    # AI-ход после явной отправки: считаем по ai_text ДО и ПОСЛЕ.
                elif t == "stt_partial":
                    note("stt_partial", text=m.get("text", "")[:60])
                elif t in ("stage", "timer", "tts_stop"):
                    note(t)

        rd = asyncio.create_task(reader())

        # 4) Стриминг речи в реальном времени (чанки 250 мс + паузы).
        # Паузы — чанки ТИШИНЫ (нули): VAD voice считает аудио-время, не wall —
        # без тишины final по VAD_MIN_SILENCE_MS не придёт.
        silence_chunk = b"\x00" * int(CHUNK_S * 32000)
        for i, pcm in enumerate(pcms):
            for off in range(0, len(pcm) - 1, int(CHUNK_S * 32000)):
                chunk = pcm[off:off + int(CHUNK_S * 32000)]
                await ws.send(chunk)
                await asyncio.sleep(CHUNK_S)
            if i < len(pcms) - 1:
                for _ in range(int(PAUSE_S / CHUNK_S)):
                    await ws.send(silence_chunk)
                    await asyncio.sleep(CHUNK_S)
        note("stream_done")

        # Даём VAD закрыть последний сегмент (хвост тишины 600 мс).
        await asyncio.sleep(2.0)

        # 5) recording off + utterance (склеенный текст сегментов — как клиент).
        merged = " ".join(segments).strip()
        ai_text_before = sum(1 for e in timeline if e["kind"] == "ai_text")
        user_transcripts_before = user_transcripts  # до явной отправки — 0 ожидаем
        await ws.send(json.dumps({"type": "ui", "name": "recording", "payload": {"on": False}}))
        note("recording_off", segments=len(segments), total_speech_ms=total_speech_ms)
        if merged:
            await ws.send(json.dumps({"type": "ui", "name": "utterance", "payload": {"text": merged}}))
            note("utterance", chars=len(merged))

        # 6) Ждём ответ ИИ (ai_text после отправки) — до 60 с (LLM-узел).
        for _ in range(60):
            await asyncio.sleep(1)
            if sum(1 for e in timeline if e["kind"] == "ai_text") > ai_text_before:
                break
        ai_turns = sum(1 for e in timeline if e["kind"] == "ai_text") - ai_text_before

        # finish.
        await ws.send(json.dumps({"type": "ui", "name": "finish"}))
        await asyncio.sleep(1.0)
        rd.cancel()

    summary = {
        "task": "T-20261009164629 live-проверка режима записи (FR-S8)",
        "session": sid,
        "audio_s": round(total_audio_s, 1),
        "n_segments": len(segments),
        "total_speech_ms": total_speech_ms,
        "merged_text": merged,
        "user_transcripts_before_send": user_transcripts_before,
        "ai_turns_after_send": ai_turns,
        "pass": len(segments) >= 2 and user_transcripts_before == 0 and ai_turns >= 1,
        "timeline": timeline,
    }
    with open(args.out, "w") as f:
        json.dump(summary, f, ensure_ascii=False, indent=2)
    print(json.dumps({k: v for k, v in summary.items() if k != "timeline"}, ensure_ascii=False, indent=2))
    print(f"PASS={summary['pass']} → {args.out}")


if __name__ == "__main__":
    asyncio.run(main())
