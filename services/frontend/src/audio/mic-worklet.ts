/**
 * Исходник AudioWorklet-процессора строкой: mic.ts встраивает его как
 * Blob-URL — отдельный статический файл не нужен.
 */
import { TARGET_RATE } from './resample';

// Экспортируется для теста (src/audio/worklet.test.ts): процессор собирается
// в реальный AudioWorkletProcessor — без базового класса у инстанса нет
// `port`, и Chrome валит процессор в processorerror (тишина → ложный переход на
// резервный захват, «AudioWorklet молчит всегда»). options.processorOptions —
// вложенный объект, flat-доступ (options.inputRate) давал undefined →
// chunkSamples undefined → чанки не постились никогда.
export const WORKLET_CODE = `
class PcmCapture extends AudioWorkletProcessor {
  constructor(options) {
    super();
    const p = (options && options.processorOptions) || {};
    // sampleRate — глобал AudioWorkletGlobalScope (фактическая частота рендера).
    this.rate = p.inputRate || sampleRate;
    this.target = p.targetRate || ${TARGET_RATE};
    this.chunkSamples = p.chunkSamples || ${Math.round(TARGET_RATE * 0.25)};
    this.acc = [];
    this.accLen = 0;
    // Уровень для эквалайзера: окно ~100 мс по входной частоте.
    this.levelWin = Math.max(1, Math.round(this.rate * 0.1));
    this.levelSum = 0;
    this.levelN = 0;
  }
  reportLevel() {
    const rms = this.levelN > 0 ? Math.sqrt(this.levelSum / this.levelN) : 0;
    this.levelSum = 0;
    this.levelN = 0;
    this.port.postMessage({ kind: 'level', rms: rms });
  }
  levelTick(n) {
    this.levelN += n;
    if (this.levelN >= this.levelWin) {
      this.reportLevel();
    }
  }
  process(inputs) {
    const ch = inputs[0] && inputs[0][0];
    if (!ch) {
      return true;
    }
    for (let i = 0; i < ch.length; i++) {
      this.levelSum += ch[i] * ch[i];
    }
    this.levelTick(ch.length);
    if (this.rate === this.target) {
      // однообразный случай обрабатывается ниже через общий путь
      this.push(ch);
      return true;
    }
    // ресемплинг линейной интерполяцией в worklet
    const ratio = this.rate / this.target;
    const outLen = Math.floor(ch.length / ratio);
    const out = new Float32Array(outLen);
    for (let i = 0; i < outLen; i++) {
      const pos = i * ratio;
      const i0 = Math.floor(pos);
      const i1 = Math.min(i0 + 1, ch.length - 1);
      const f = pos - i0;
      out[i] = ch[i0] * (1 - f) + ch[i1] * f;
    }
    this.push(out);
    return true;
  }
  push(samples) {
    this.acc.push(samples);
    this.accLen += samples.length;
    while (this.accLen >= this.chunkSamples && this.acc.length > 0) {
      const out = new Float32Array(this.chunkSamples);
      let off = 0;
      while (off < this.chunkSamples) {
        const head = this.acc[0];
        const take = Math.min(head.length, this.chunkSamples - off);
        out.set(head.subarray(0, take), off);
        off += take;
        if (take === head.length) this.acc.shift();
        else this.acc[0] = head.subarray(take);
      }
      this.accLen -= this.chunkSamples;
      // int16 в worklet
      const i16 = new Int16Array(this.chunkSamples);
      for (let i = 0; i < this.chunkSamples; i++) {
        let v = out[i];
        if (v > 1) v = 1;
        if (v < -1) v = -1;
        i16[i] = Math.round(v * 32767);
      }
      this.port.postMessage(i16);
    }
    return true;
  }
}
registerProcessor('pcm-capture', PcmCapture);
`;
