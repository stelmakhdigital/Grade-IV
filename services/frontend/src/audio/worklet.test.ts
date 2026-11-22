/**
 * Регресс (feedback 2026-11-22): основной путь захвата молчал ВСЕГДА — процессор
 * в WORKLET_CODE не наследовал AudioWorkletProcessor (у инстанса нет `port`,
 * Chrome валит его в processorerror) и читал processorOptions flat
 * (options.inputRate → undefined → chunkSamples undefined → чанки не отправлялись).
 * Тест собирает процессор так же, как это делает AudioWorkletGlobalScope, и кормит
 * его квантами по 128 отсчётов: без базового класса или с flat-опциями здесь
 * просто не будет чанков.
 */
import { describe, expect, it } from 'vitest';
import { WORKLET_CODE, chunkRms } from './mic';

const QUANTA = 128; // размер кванта в AudioWorklet

class FakeAudioWorkletProcessor {
  posted: unknown[] = [];
  port = {
    postMessage: (m: unknown): void => {
      this.posted.push(m);
    },
  };
}

/** Собрать процессор из WORKLET_CODE и прогоать по нему `quantaN` квантов. */
function run(opts: {
  inputRate: number;
  sampleRateGlobal: number;
  quantaN: number;
  processorOptions?: Record<string, number>;
}): { chunks: Int16Array[]; levels: unknown[] } {
  let Ctor: any = null;
  const registerProcessor = (_name: string, ctor: unknown): void => {
    Ctor = ctor;
  };
  // Инжектируем глобалы AudioWorkletGlobalScope аргументами функции.
  new Function(
    'registerProcessor',
    'AudioWorkletProcessor',
    'sampleRate',
    WORKLET_CODE,
  )(registerProcessor, FakeAudioWorkletProcessor, opts.sampleRateGlobal);
  expect(Ctor).not.toBeNull();

  const inst = new Ctor({
    numberOfInputs: 1,
    numberOfOutputs: 1,
    outputChannelCount: [1],
    processorOptions: opts.processorOptions,
  });
  const ch = new Float32Array(QUANTA);
  for (let i = 0; i < QUANTA; i++) ch[i] = Math.sin(i / 8) * 0.4;
  for (let k = 0; k < opts.quantaN; k++) inst.process([[ch]]);

  const posted: unknown[] = inst.posted;
  return {
    chunks: posted.filter((m): m is Int16Array => m instanceof Int16Array),
    levels: posted.filter((m) => !(m instanceof Int16Array)),
  };
}

describe('WORKLET_CODE (pcm-capture)', () => {
  it('гонит чанки PCM16 и уровни при ресемплинге 48→16 кГц', () => {
    // 3 чанка по 4000 отсчётов 16 кГц = 0,75 с → 36000 отсчётов 48 кГц ≈ 282 кванта.
    const { chunks, levels } = run({
      inputRate: 48000,
      sampleRateGlobal: 48000,
      quantaN: 300,
      processorOptions: { inputRate: 48000, targetRate: 16000, chunkSamples: 4000 },
    });
    expect(chunks.length).toBeGreaterThanOrEqual(3);
    expect(chunks[0]).toHaveLength(4000);
    // Не тишина: через процессор прошёл реальный сигнал (синус 0,4 → rms ~0,28).
    expect(chunkRms(chunks[0])).toBeGreaterThan(0.1);
    expect(levels.length).toBeGreaterThan(0);
  });

  it('чанки идут и без processorOptions (дефолты из глобала sampleRate)', () => {
    const { chunks } = run({
      inputRate: 44100,
      sampleRateGlobal: 44100,
      quantaN: 400,
    });
    expect(chunks.length).toBeGreaterThanOrEqual(3);
    expect(chunks[0]).toHaveLength(4000);
  });
});
