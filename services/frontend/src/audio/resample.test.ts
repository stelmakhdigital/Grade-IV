import { describe, expect, it } from 'vitest';
import {
  parseTtsFrame,
  pcmDurationS,
  resampleToPcm16,
  TARGET_RATE,
} from './resample';

describe('resampleToPcm16', () => {
  it('16 кГц — без изменения длины, квантование', () => {
    const in16 = new Float32Array([0, 0.5, -1, 1, 0.999]);
    const out = resampleToPcm16(in16, 16000);
    expect(out.length).toBe(5);
    expect(out[0]).toBe(0);
    expect(out[1]).toBe(16384); // 0.5*32767=16383.5 -> 16384
    expect(out[2]).toBe(-32767);
    expect(out[3]).toBe(32767);
  });

  it('48 кГц → 16 кГц: длина /3, плавный сигнал', () => {
    // синус 0.25 периода на 3 образца-единицы
    const in48 = new Float32Array([0, 1, 0]);
    const out = resampleToPcm16(in48, 48000);
    expect(out.length).toBe(1);
    expect(out[0]).toBe(0); // интерполяция 0→1 в точке 0.333
  });

  it('насыщение и NaN', () => {
    const out = resampleToPcm16(new Float32Array([2, -2, Number.NaN]), 16000);
    expect(out[0]).toBe(32767);
    expect(out[1]).toBe(-32767);
    expect(out[2]).toBe(0);
  });

  it('пустой вход', () => {
    expect(resampleToPcm16(new Float32Array(0), 16000).length).toBe(0);
  });
});

describe('parseTtsFrame', () => {
  it('заголовок {seq,flags LE} + PCM16, end-флаг', () => {
    // seq=7, flags=1 (end), pcm = [100, -100]
    const buf = new ArrayBuffer(4 + 4);
    const v = new DataView(buf);
    v.setUint16(0, 7, true);
    v.setUint16(2, 1, true);
    v.setInt16(4, 100, true);
    v.setInt16(6, -100, true);
    const frame = parseTtsFrame(buf);
    expect(frame).not.toBeNull();
    expect(frame?.pcm).toEqual(new Int16Array([100, -100]));
    expect(frame?.isLast).toBe(true);
  });

  it('без end-флага и короткий кадр', () => {
    const buf = new ArrayBuffer(4 + 2);
    const v = new DataView(buf);
    v.setUint16(0, 0, true);
    v.setUint16(2, 0, true);
    expect(parseTtsFrame(buf)?.isLast).toBe(false);
    expect(parseTtsFrame(new ArrayBuffer(2))).toBeNull();
  });
});

describe('pcmDurationS', () => {
  it('250 мс на 4000 образцах', () => {
    expect(pcmDurationS(new Int16Array(4000))).toBeCloseTo(0.25, 5);
    expect(TARGET_RATE).toBe(16000);
  });
});
