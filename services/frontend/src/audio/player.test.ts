/**
 * Тест PcmPlayer (баг-фикс «звук обрывается после первых слов»):
 * TTS-кадры приходят потоком, burst-ами по фразам. Каждый кадр обязан
 * попадать в планирование AudioContext (pump), иначе очередь висит
 * невоспроизведённой. До фикса pump вызывался только при первом кадре.
 */
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { PcmPlayer } from './player';

// Мок AudioContext (jsdom не имеет Web Audio).
class MockSource {
  constructor(private t: () => number) {}
  startedAt: number | null = null;
  connect() { return this; }
  start(at?: number) { this.startedAt = at ?? this.t(); }
}
class MockBuffer {
  constructor(public length: number, public sampleRate: number) {}
  copyToChannel() {}
}
class MockCtx {
  state = 'running' as AudioContextState;
  currentTime = 0;
  sampleRate = 48000;
  destination = {};
  resume() { return Promise.resolve(); }
  close() { return Promise.resolve(); }
  createBuffer(_ch: number, len: number, rate: number) { return new MockBuffer(len, rate); }
  createBufferSource() { return new MockSource(() => this.currentTime); }
}

let ctx: MockCtx;
beforeEach(() => {
  ctx = new MockCtx();
  (globalThis as any).AudioContext = class {
    constructor() { return ctx; }
  };
});

const frame = (n = 4000) => new Int16Array(n); // 250 мс @16 кГц

describe('PcmPlayer', () => {
  it('воспроизводит ВСЕ кадры, пришедшие отдельными burst-ами (поток TTS)', () => {
    const p = new PcmPlayer();
    const srcs: MockSource[] = [];
    const origCreate = ctx.createBufferSource.bind(ctx);
    ctx.createBufferSource = () => {
      const s = origCreate();
      srcs.push(s as unknown as MockSource);
      return s;
    };

    // Burst 1: три кадра подряд (первая фраза TTS).
    p.playChunk(frame());
    p.playChunk(frame());
    p.playChunk(frame());

    // Пауза: контекст «идёт» (как в реальном плеере), playing=true.
    ctx.currentTime = 0.8;

    // Burst 2: следующая фраза — кадры приходят, пока идёт воспроизведение.
    p.playChunk(frame());
    p.playChunk(frame());

    // До фикса: 5-6 кадры никогда не стартовали (pump не вызывался).
    expect(srcs.length).toBe(5);
    for (const s of srcs) {
      expect(s.startedAt).not.toBeNull();
    }
    // Старты монотонно растут (незаметная склейка, без наложений).
    const starts = srcs.map((s) => s.startedAt as number);
    for (let i = 1; i < starts.length; i++) {
      expect(starts[i]).toBeGreaterThan(starts[i - 1]);
    }
    // Общая длительность ≈ 5 × 0.25 с = 1.25 с (с погрешностью ресемплинга).
    const total = starts[starts.length - 1] + 1.25 - starts[0];
    expect(total).toBeGreaterThan(1.0);
  });

  it('не дублирует старты при повторном playChunk во время паузы', () => {
    const p = new PcmPlayer();
    const srcs: MockSource[] = [];
    const origCreate = ctx.createBufferSource.bind(ctx);
    ctx.createBufferSource = () => {
      const s = origCreate();
      srcs.push(s as unknown as MockSource);
      return s;
    };
    p.playChunk(frame());
    const n1 = srcs.length;
    // Повторный кадр — один новый старт, старые не трогаем.
    p.playChunk(frame());
    expect(srcs.length).toBe(n1 + 1);
  });
});
