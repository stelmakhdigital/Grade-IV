import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  MicCapture,
  SilenceDetector,
  SILENCE_MS,
  SILENCE_RMS,
  chunkRms,
} from './mic';

// --- чистый детектор тишины (время передаётся аргументом) ------------------

describe('SilenceDetector', () => {
  const silentChunk = 250; // мс

  it('тихий микрофон: тишина ≥ SILENCE_MS → onMuted ровно один раз', () => {
    const onMuted = vi.fn();
    const onRecover = vi.fn();
    const d = new SilenceDetector({ onMuted, onRecover });
    let t = 0;
    for (; t <= SILENCE_MS; t += silentChunk) d.feed(0, t);
    expect(onMuted).toHaveBeenCalledTimes(1);
    expect(onRecover).not.toHaveBeenCalled();
    // тишина продолжается — повторных предупреждений за период нет
    for (let i = 0; i < 10; i++) d.feed(0, t + i * silentChunk);
    expect(onMuted).toHaveBeenCalledTimes(1);
  });

  it('короткая пауза (< SILENCE_MS) → намерения нет', () => {
    const onMuted = vi.fn();
    const d = new SilenceDetector({ onMuted, onRecover: vi.fn() });
    let t = 0;
    for (; t < SILENCE_MS; t += silentChunk) d.feed(0, t);
    expect(onMuted).not.toHaveBeenCalled();
    d.feed(SILENCE_RMS, t + silentChunk); // заговорил до конца паузы
    expect(onMuted).not.toHaveBeenCalled();
  });

  it('оживший микрофон: onRecover, повторное молчание предупреждает заново', () => {
    const onMuted = vi.fn();
    const onRecover = vi.fn();
    const d = new SilenceDetector({ onMuted, onRecover });
    let t = 0;
    for (; t <= SILENCE_MS; t += silentChunk) d.feed(0, t);
    expect(onMuted).toHaveBeenCalledTimes(1);
    d.feed(0.1, t + silentChunk);
    expect(onRecover).toHaveBeenCalledTimes(1);
    // новый период тишины → новое предупреждение
    t = 0;
    for (; t <= SILENCE_MS; t += silentChunk) d.feed(0, t);
    expect(onMuted).toHaveBeenCalledTimes(2);
    expect(onRecover).toHaveBeenCalledTimes(1);
  });
});

describe('chunkRms', () => {
  it('нули → 0', () => {
    expect(chunkRms(new Int16Array(16000))).toBe(0);
  });

  it('сигнал (амплитуда 0.5) ≥ SILENCE_RMS', () => {
    const pcm = new Int16Array(16000);
    for (let i = 0; i < pcm.length; i++) {
      pcm[i] = Math.round(0.5 * 32767 * Math.sin((2 * Math.PI * 440 * i) / 16000));
    }
    expect(chunkRms(pcm)).toBeGreaterThanOrEqual(SILENCE_RMS);
  });
});

// --- MicCapture: fallback-переход и «молчание» через реальный путь ----------

class FakeWorkletNode {
  port: { onmessage: ((e: { data: unknown }) => void) | null } = { onmessage: null };
  connect = vi.fn();
  disconnect = vi.fn();
  post(data: unknown): void {
    this.port.onmessage?.({ data });
  }
}

class FakeSPNode {
  onaudioprocess: ((e: { inputBuffer: unknown; outputBuffer: unknown }) => void) | null = null;
  connect = vi.fn();
  disconnect = vi.fn();
}

class FakeCtx {
  // Статика: MicCapture создаёт собственный экземпляр AudioContext.
  static createdSP: FakeSPNode[] = [];
  sampleRate = 48000;
  audioWorklet = { addModule: vi.fn(async () => undefined) };
  destination = {};
  createMediaStreamSource = () => ({ connect: vi.fn() });
  createGain = () => ({ gain: { value: 1 }, connect: vi.fn() });
  createScriptProcessor = () => {
    const n = new FakeSPNode();
    FakeCtx.createdSP.push(n);
    return n;
  };
  close = vi.fn(async () => undefined);
}

function buffer(data: Float32Array): { getChannelData: (ch: number) => Float32Array } {
  return { getChannelData: () => data };
}

function spEvent(zeros: Float32Array): { inputBuffer: unknown; outputBuffer: unknown } {
  return { inputBuffer: buffer(zeros), outputBuffer: buffer(zeros) };
}

describe('MicCapture: fallback и «молчащий» микрофон', () => {
  let ctx: FakeCtx;
  let worklets: FakeWorkletNode[];

  beforeEach(() => {
    vi.useFakeTimers();
    FakeCtx.createdSP = [];
    ctx = new FakeCtx();
    worklets = [];
    vi.stubGlobal('AudioContext', FakeCtx);
    vi.stubGlobal('AudioWorkletNode', class extends FakeWorkletNode {
      constructor() {
        super();
        worklets.push(this);
      }
    });
    const fakeStream = { getTracks: () => [{ stop: vi.fn() }] } as unknown as MediaStream;
    vi.stubGlobal('navigator', {
      mediaDevices: { getUserMedia: vi.fn(async () => fakeStream) },
    });
    // jsdom не знает createObjectURL (worklet-модуль в Blob-URL).
    (URL as unknown as { createObjectURL: (b: Blob) => string }).createObjectURL = () => 'blob:fake';
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('worklet без чанков 3 с → fallback активен + info-сообщение (критерий 2)', async () => {
    const info: string[] = [];
    const errors: string[] = [];
    const mic = new MicCapture();
    await mic.start({ onChunk: () => {}, onInfo: (m) => info.push(m), onError: (e) => errors.push(e) });
    expect(FakeCtx.createdSP.length).toBe(0);

    vi.advanceTimersByTime(3000);

    expect(FakeCtx.createdSP.length).toBe(1); // ScriptProcessor поднят
    expect(info).toEqual([
      'Микрофон: основной путь (AudioWorklet) молчит, включён резервный захват',
    ]);
    expect(errors).toEqual([]); // info — не ошибка
    mic.stop();
  });

  it('тихий вход через fallback: ≥ SILENCE_MS тишины → muted + onError, затем оживает', async () => {
    const states: string[] = [];
    const errors: string[] = [];
    const chunks: Int16Array[] = [];
    const mic = new MicCapture();
    await mic.start({
      onChunk: (c) => chunks.push(c),
      onState: (s) => states.push(s),
      onError: (e) => errors.push(e),
    });
    vi.advanceTimersByTime(3000); // fallback
    const sp = FakeCtx.createdSP[0];
    expect(sp).toBeDefined();

    const zeros = new Float32Array(4096);
    // 5.5 с тишины по 100 мс (Date под fake-таймерами)
    for (let t = 0; t <= 5500; t += 100) {
      sp.onaudioprocess?.(spEvent(zeros));
      vi.advanceTimersByTime(100);
    }
    expect(states).toContain('muted');
    expect(errors).toContain(
      'Микрофон молчит: проверьте устройство, мьют и разрешения браузера. ' +
        'Диагностика: откройте /audio-debug.html и проверьте уровень сигнала с микрофона.',
    );
    // повторных предупреждений за период нет
    expect(errors.filter((e) => e.startsWith('Микрофон молчит')).length).toBe(1);

    // оживление: сигнал → running
    const tone = new Float32Array(4096);
    for (let i = 0; i < tone.length; i++) tone[i] = 0.5 * Math.sin((2 * Math.PI * 440 * i) / 48000);
    sp.onaudioprocess?.(spEvent(tone));
    expect(states).toContain('running');
    mic.stop();
  });
});
