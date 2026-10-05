/**
 * Тест MicCapture: getUserMedia-констрейнты включают echoCancellation +
 * noiseSuppression (barge-in: микрофон слушается и во время речи ИИ —
 * эхо динамика→микрофон подавляется браузером, см. ADR-002/SRS §8).
 */
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { MicCapture } from './mic';

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
