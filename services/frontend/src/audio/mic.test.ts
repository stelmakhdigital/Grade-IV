/**
 * Тест MicCapture: getUserMedia-констрейнты включают echoCancellation +
 * noiseSuppression (barge-in: микрофон слушается и во время речи ИИ —
 * эхо динамика→микрофон подавляется браузером, см. ADR-002/SRS §8).
 */
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { MicCapture } from './mic';

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
