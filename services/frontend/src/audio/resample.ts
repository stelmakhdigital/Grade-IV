/**
 * Чистые аудио-функции (WP-8): ресэмплинг в PCM16 16 кГц и
 * работа с бинарными кадрами TTS {seq u16 LE, flags u16 LE} + PCM16.
 * Чистые (без DOM) — юнит-тестируются в jsdom.
 */

export const TARGET_RATE = 16000;
export const TTS_HEADER_BYTES = 4;
export const TTS_FLAG_END = 0x01;

/**
 * Линейное ресэмплирование float32 (inputRate) в int16 (16 кГц).
 * Квантование со сатурацией в [-32768, 32767].
 */
export function resampleToPcm16(
  input: Float32Array,
  inputRate: number,
): Int16Array {
  if (input.length === 0) return new Int16Array(0);
  if (inputRate === TARGET_RATE) {
    const out = new Int16Array(input.length);
    for (let i = 0; i < input.length; i++) {
      out[i] = clampPcm(input[i]);
    }
    return out;
  }
  const ratio = inputRate / TARGET_RATE;
  const outLen = Math.floor(input.length / ratio);
  const out = new Int16Array(outLen);
  for (let i = 0; i < outLen; i++) {
    const pos = i * ratio;
    const i0 = Math.floor(pos);
    const i1 = Math.min(i0 + 1, input.length - 1);
    const frac = pos - i0;
    out[i] = clampPcm(input[i0] * (1 - frac) + input[i1] * frac);
  }
  return out;
}

function clampPcm(v: number): number {
  if (Number.isNaN(v)) return 0;
  if (v > 1) v = 1;
  if (v < -1) v = -1;
  const s = Math.round(v * 32767);
  return s > 32767 ? 32767 : s;
}

/**
 * Бинарный кадр TTS → PCM16-тело (заголовок {seq, flags} отброшен) и
 * признак последнего кадра потока. Возвращает null, если кадр короче заголовка.
 */
export function parseTtsFrame(buf: ArrayBuffer): {
  pcm: Int16Array;
  isLast: boolean;
} | null {
  if (buf.byteLength < TTS_HEADER_BYTES) return null;
  const view = new DataView(buf);
  const flags = view.getUint16(2, true);
  const pcm = new Int16Array(buf, TTS_HEADER_BYTES, (buf.byteLength - TTS_HEADER_BYTES) / 2);
  return { pcm, isLast: (flags & TTS_FLAG_END) !== 0 };
}

/** PCM16 (Int16Array) → ArrayBuffer (для AudioBuffer.copyToChannel). */
export function pcmToBuffer(pcm: Int16Array): ArrayBuffer {
  return pcm.buffer.slice(pcm.byteOffset, pcm.byteOffset + pcm.byteLength);
}

/** Длительность PCM16-данных в секундах (16 кГц mono). */
export function pcmDurationS(pcm: Int16Array): number {
  return pcm.length / TARGET_RATE;
}

/**
 * Линейное ресэмплирование float32 → float32 между произвольными частотами
 * (для плеера: 16 кГц TTS → частота AudioContext устройства).
 */
export function resampleFloat(
  input: Float32Array,
  fromRate: number,
  toRate: number,
): Float32Array {
  if (input.length === 0) return new Float32Array(0);
  if (fromRate === toRate) return input;
  const ratio = fromRate / toRate;
  const outLen = Math.floor(input.length / ratio);
  const out = new Float32Array(outLen);
  for (let i = 0; i < outLen; i++) {
    const pos = i * ratio;
    const i0 = Math.floor(pos);
    const i1 = Math.min(i0 + 1, input.length - 1);
    const frac = pos - i0;
    out[i] = input[i0] * (1 - frac) + input[i1] * frac;
  }
  return out;
}
