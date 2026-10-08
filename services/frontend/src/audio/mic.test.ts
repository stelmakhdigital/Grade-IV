/**
 * Тест MicCapture: getUserMedia-констрейнты включают echoCancellation +
 * noiseSuppression (barge-in: микрофон слушается и во время речи ИИ —
 * эхо динамика→микрофон подавляется браузером, см. ADR-002/SRS §8).
 */
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { MicCapture, MAX_REINIT, SILENCE_MS, type MicCaptureEvents } from './mic';

/** Доступ к приватным полям/методам MicCapture (dev-диагностика). */
type MicInternal = {
  micState: string;
  dbgGaps: number[];
  dbgChunks: number;
  dbgRms: number;
  dbgLastTs: number;
  feedSilence(pcm: Int16Array): void;
};

describe('MicCapture constraints', () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
  });

  it('запрашивает echoCancellation и noiseSuppression у браузера', async () => {
    const fakeStream = { getTracks: () => [{ stop: vi.fn() }] } as unknown as MediaStream;
    const getUserMedia = vi.fn(async (_c?: MediaStreamConstraints) => fakeStream);
    vi.stubGlobal('navigator', { mediaDevices: { getUserMedia } });

    const mic = new MicCapture();
    // AudioContext в jsdom нет — старт упадёт после getUserMedia;
    // проверяем только constrейнты запроса микрофона.
    await expect(mic.start({ onChunk: () => {}, onError: () => {} })).rejects.toBeTruthy();

    expect(getUserMedia).toHaveBeenCalledTimes(1);
    const constraints = getUserMedia.mock.calls[0][0] as MediaStreamConstraints;
    const audio = constraints.audio as Record<string, unknown>;
    expect(audio.echoCancellation).toBe(true);
    expect(audio.noiseSuppression).toBe(true);
  });
});

describe('MicCapture.debugInfo / feedSilence (dev-диагностика)', () => {
  const internal = (cap: MicCapture): MicInternal => cap as unknown as MicInternal;

  it('пустая статистика: chunks/medGapMs/maxRms = 0, path по умолчанию', () => {
    expect(new MicCapture().debugInfo()).toEqual({
      path: 'worklet',
      ctxState: null,
      rate: 0,
      chunks: 0,
      medGapMs: 0,
      maxRms: 0,
    });
  });

  it('feedSilence вне running/muted (idle) чанки не учитывает', () => {
    const cap = new MicCapture();
    internal(cap).feedSilence(new Int16Array(4000));
    expect(cap.debugInfo().chunks).toBe(0);
  });

  it('running: считает чанки, max rms и медиану межчанковых gap-ов', () => {
    vi.useFakeTimers();
    try {
      vi.setSystemTime(1_000_000);
      const cap = new MicCapture();
      internal(cap).micState = 'running';
      const quiet = new Int16Array(4000);
      const loud = new Int16Array(4000).fill(16000); // rms ≈ 0.4883

      internal(cap).feedSilence(quiet); // первый чанк: gap не записывается
      vi.setSystemTime(1_000_100);
      internal(cap).feedSilence(quiet); // gap 100 мс
      vi.setSystemTime(1_000_400);
      internal(cap).feedSilence(loud); // gap 300 мс, максимальный rms
      vi.setSystemTime(1_000_600);
      internal(cap).feedSilence(quiet); // gap 200 мс

      const info = cap.debugInfo();
      expect(info.path).toBe('worklet');
      expect(info.ctxState).toBeNull();
      expect(info.chunks).toBe(4);
      // gap-ы [100, 300, 200] → отсортировано [100, 200, 300], idx floor(3/2)=1
      expect(info.medGapMs).toBe(200);
      expect(info.maxRms).toBeCloseTo(16000 / 32768);
    } finally {
      vi.useRealTimers();
    }
  });

  it('muted: чанки тоже учитываются (поиск возврата уровня)', () => {
    const cap = new MicCapture();
    internal(cap).micState = 'muted';
    internal(cap).feedSilence(new Int16Array(4000).fill(100));
    const info = cap.debugInfo();
    expect(info.chunks).toBe(1);
    expect(info.maxRms).toBeCloseTo(100 / 32768);
  });

  it('gap ≥ 30000 мс в статистику не записывается', () => {
    vi.useFakeTimers();
    try {
      vi.setSystemTime(1_000_000);
      const cap = new MicCapture();
      internal(cap).micState = 'running';
      const pcm = new Int16Array(4000);
      internal(cap).feedSilence(pcm);
      vi.setSystemTime(1_000_000 + 60_000);
      internal(cap).feedSilence(pcm); // gap 60 с — вне окна 30 с
      expect(internal(cap).dbgGaps).toHaveLength(0);
      expect(cap.debugInfo().chunks).toBe(2);
      expect(cap.debugInfo().medGapMs).toBe(0);
    } finally {
      vi.useRealTimers();
    }
  });

  it('dbgGaps хранит не более 8 последних gap-ов', () => {
    vi.useFakeTimers();
    try {
      vi.setSystemTime(1050);
      const cap = new MicCapture();
      internal(cap).micState = 'running';
      internal(cap).dbgGaps = [10, 20, 30, 40, 50, 60, 70, 80];
      internal(cap).dbgLastTs = 100; // новый gap = 1050 − 100 = 950 мс
      internal(cap).feedSilence(new Int16Array(4000));
      expect(internal(cap).dbgGaps).toEqual([20, 30, 40, 50, 60, 70, 80, 950]);
    } finally {
      vi.useRealTimers();
    }
  });

  it('medGapMs — медиана из записанных gap-ов (dbgGaps задан напрямую)', () => {
    const cap = new MicCapture();
    internal(cap).dbgGaps = [400, 100, 300, 200]; // отсортировано [100,200,300,400]
    expect(cap.debugInfo().medGapMs).toBe(300); // idx floor(4/2)=2
  });
});

// --- цепочка надёжности захвата: worklet → fallback → полная reinit (T-20261008175905)

describe('MicCapture: цепочка надёжности (worklet → fallback → reinit)', () => {
  class FakeWorkletNode {
    static instances: FakeWorkletNode[] = [];
    port: { onmessage: ((e: { data: unknown }) => void) | null } = { onmessage: null };
    connect = vi.fn();
    disconnect = vi.fn();
    constructor() {
      FakeWorkletNode.instances.push(this);
    }
    /** Симуляция доставки чанка PCM16 из worklet. */
    sendChunk(pcm: Int16Array): void {
      this.port.onmessage?.({ data: pcm });
    }
  }

  class FakeSPNode {
    onaudioprocess: ((e: unknown) => void) | null = null;
    connect = vi.fn();
    disconnect = vi.fn();
  }

  class FakeCtx {
    static createdSP: FakeSPNode[] = [];
    sampleRate = 48000;
    state = 'running';
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

  interface Harness {
    mic: MicCapture;
    getUserMedia: ReturnType<typeof vi.fn>;
    info: string[];
    errors: string[];
    states: string[];
  }

  /**
   * Старт захвата в fake-среде (jsdom): getUserMedia — управляемый,
   * AudioContext/AudioWorklet — моки. gum(calls) — что вернуть на N-ном вызове
   * (брось исключение, чтобы симулировать отказ доступа).
   */
  async function beginMic(gum?: (calls: number) => unknown): Promise<Harness> {
    FakeWorkletNode.instances = [];
    FakeCtx.createdSP = [];
    const info: string[] = [];
    const errors: string[] = [];
    const states: string[] = [];
    const stream = { getTracks: () => [{ stop: vi.fn() }] } as unknown as MediaStream;
    let calls = 0;
    const getUserMedia = vi.fn(async () => {
      calls++;
      // gum возвращает значение (undefined → дефолтный стрим) или бросает (отказ).
      return gum ? (gum(calls) ?? stream) : stream;
    });
    vi.stubGlobal('navigator', { mediaDevices: { getUserMedia } });
    vi.stubGlobal('AudioContext', FakeCtx);
    vi.stubGlobal('AudioWorkletNode', class extends FakeWorkletNode {});
    // jsdom не знает createObjectURL (worklet-модуль в Blob-URL).
    (URL as unknown as { createObjectURL: (b: Blob) => string }).createObjectURL = () => 'blob:fake';
    const mic = new MicCapture();
    const events: MicCaptureEvents = {
      onChunk: () => {},
      onInfo: (m) => info.push(m),
      onError: (e) => errors.push(e),
      onState: (s) => states.push(s),
    };
    await mic.start(events);
    return { mic, getUserMedia, info, errors, states };
  }

  const advance = (ms: number) => vi.advanceTimersByTimeAsync(ms);
  /** Дождаться завершения асинхронной цепочки reinit (микрозадачи). */
  const flush = async () => {
    for (let i = 0; i < 20; i++) await Promise.resolve();
  };

  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('worklet молчит 3 с → fallback (ScriptProcessor) + info', async () => {
    const h = await beginMic();
    expect(h.mic.debugInfo().path).toBe('worklet');
    await advance(3000);
    expect(FakeCtx.createdSP).toHaveLength(1);
    expect(h.mic.debugInfo().path).toBe('fallback');
    expect(h.info).toEqual(['Микрофон: основной путь (AudioWorklet) молчит, включён резервный захват']);
    expect(h.errors).toEqual([]); // info — не ошибка
  });

  it('fallback молчит 3 с → полная повторная инициализация: getUserMedia заново, running, onInfo с попыткой', async () => {
    const h = await beginMic();
    await advance(3000); // worklet молчит → fallback
    expect(h.mic.debugInfo().path).toBe('fallback');
    await advance(3000); // fallback молчит → reinit #1
    await flush();
    expect(h.getUserMedia).toHaveBeenCalledTimes(2);
    expect(FakeWorkletNode.instances).toHaveLength(2); // новый worklet-путь
    expect(h.mic.state).toBe('running');
    expect(h.mic.debugInfo().path).toBe('worklet');
    expect(h.info).toContain('Микрофон: резервный путь молчит — повторяю инициализацию (попытка 1)');
    expect(h.errors).toEqual([]);
  });

  it(`${MAX_REINIT} пересозданий подряд → финальный onError со ссылкой на audio-debug.html`, async () => {
    const h = await beginMic();
    // 6 тишинных окон по 3 с: worklet→fallback, →reinit#1, worklet→fallback,
    // →reinit#2, worklet→fallback, → лимит исчерпан.
    for (let i = 0; i < 2 * (MAX_REINIT + 1); i++) {
      await advance(3000);
      await flush();
    }
    expect(h.getUserMedia).toHaveBeenCalledTimes(1 + MAX_REINIT);
    expect(h.info.filter((m) => m.includes('повторяю инициализацию'))).toHaveLength(MAX_REINIT);
    expect(h.errors.at(-1)).toContain('/audio-debug.html');
    expect(h.errors.at(-1)).toContain('повторные инициализации не помогли');
  });

  it('ретрай с отклонённым getUserMedia → state "denied" + onError', async () => {
    const h = await beginMic((calls) => {
      if (calls >= 2) throw new Error('NotAllowedError');
      return undefined;
    });
    expect(h.mic.state).toBe('running');
    await advance(3000); // → fallback
    await advance(3000); // → reinit #1: getUserMedia отклонён
    await flush();
    expect(h.mic.state).toBe('denied');
    expect(h.states.at(-1)).toBe('denied');
    expect(h.errors.at(-1)).toContain('Нет доступа к микрофону');
  });

  it('первый чанок после ретрая — счётчик сброшен, повторных пересозданий нет', async () => {
    const h = await beginMic();
    await advance(3000); // → fallback
    await advance(3000); // → reinit #1
    await flush();
    expect(h.getUserMedia).toHaveBeenCalledTimes(2);
    // первый чанок с нового worklet-пути
    FakeWorkletNode.instances.at(-1)!.sendChunk(new Int16Array(4000).fill(1000));
    expect(h.mic.state).toBe('running');
    // дальнейшие 6 тишинных окон — пересозданий нет (таймер снят, счётчик 0)
    await advance(3000 * 6);
    await flush();
    expect(h.getUserMedia).toHaveBeenCalledTimes(2);
    expect(h.errors).toEqual([]);
  });

  it('muted: onError содержит инструкцию и ссылку на audio-debug.html', async () => {
    const h = await beginMic();
    const silence = (h.mic as unknown as { silence: { feed(rms: number, now: number): void } | null }).silence;
    expect(silence).not.toBeNull();
    silence!.feed(0, 100_000);
    silence!.feed(0, 100_000 + SILENCE_MS); // тишина ≥ SILENCE_MS → onMuted
    expect(h.mic.state).toBe('muted');
    expect(h.states.at(-1)).toBe('muted');
    expect(h.errors.at(-1)).toContain('/audio-debug.html');
    expect(h.errors.at(-1)).toContain('Микрофон молчит');
  });
});
