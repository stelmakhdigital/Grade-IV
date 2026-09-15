/**
 * Микрофон кандидата (WP-8, ADR-002): getUserMedia → AudioContext →
 * AudioWorklet (ресэмплинг до 16 кГц) → чанки PCM16 ~250 мс → WS.
 * Worklet-скрипт встраивается как Blob-URL — отдельный файл не нужен.
 */
import { resampleToPcm16, TARGET_RATE } from './resample';

export interface MicCaptureEvents {
  /** Чанк PCM16 16 кГц (обычно 4000 образцов = 250 мс). */
  onChunk: (pcm: Int16Array) => void;
  /** Уровень микрофона (RMS float -1..1) каждые ~100 мс — эквалайзер UI. */
  onLevel?: (rms: number) => void;
  onState?: (state: MicState) => void;
  onError?: (err: string) => void;
}

export type MicState = 'idle' | 'running' | 'stopped' | 'denied';

const WORKLET_CODE = `
class PcmCapture {
  constructor(params) {
    this.rate = params.inputRate;
    this.target = params.targetRate;
    this.chunkSamples = params.chunkSamples;
    this.acc = [];
    this.accLen = 0;
    // Уровень для эквалайзера: окно ~100 мс по входной частоте.
    this.levelWin = Math.max(1, Math.round(params.inputRate * 0.1));
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
      return [true];
    }
    for (let i = 0; i < ch.length; i++) {
      this.levelSum += ch[i] * ch[i];
    }
    this.levelTick(ch.length);
    if (this.rate === this.target) {
      // однообразный случай обрабатывается ниже через общий путь
      this.push(ch);
      return [true];
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
    return [true];
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
    return [true];
  }
}
registerProcessor('pcm-capture', PcmCapture);
`;

export class MicCapture {
  private ctx: AudioContext | null = null;
  private stream: MediaStream | null = null;
  private node: AudioWorkletNode | null = null;
  private inputRate = 0;
  private micState: MicState = 'idle';

  async start(events: MicCaptureEvents): Promise<void> {
    if (this.micState === 'running') return;
    const stream = await navigator.mediaDevices.getUserMedia({
      audio: {
        channelCount: 1,
        // 16 кГц — желательно, но браузер может вернуть системную частоту;
        // ресэмплинг в worklet работает в обоих случаях.
        sampleRate: TARGET_RATE,
      },
    });
    this.stream = stream;
    const Ctx = window.AudioContext ?? (window as any).webkitAudioContext;
    const ctx = new Ctx();
    this.ctx = ctx;
    this.inputRate = ctx.sampleRate;
    await ctx.audioWorklet.addModule(
      URL.createObjectURL(new Blob([WORKLET_CODE], { type: 'application/javascript' })),
    );
    const source = ctx.createMediaStreamSource(stream);
    const node = new AudioWorkletNode(ctx, 'pcm-capture', {
      numberOfInputs: 1,
      numberOfOutputs: 1,
      outputChannelCount: [1],
      processorOptions: {
        inputRate: this.inputRate,
        targetRate: TARGET_RATE,
        chunkSamples: Math.round(TARGET_RATE * 0.25), // 250 мс
      },
    });
    node.port.onmessage = (e: MessageEvent) => {
      const d = e.data;
      if (d instanceof Int16Array) {
        events.onChunk(d);
      } else if (d && typeof d === 'object' && (d as { kind?: string }).kind === 'level') {
        events.onLevel?.((d as { rms: number }).rms);
      }
    };
    source.connect(node);
    // Заземление: gain 0, чтобы worklet работал, но звук не шёл в динамики.
    const silence = ctx.createGain();
    silence.gain.value = 0;
    node.connect(silence);
    silence.connect(ctx.destination);
    this.node = node;
    this.setState('running', events);
  }

  stop(): void {
    if (this.micState === 'idle') return;
    this.node?.disconnect();
    this.node = null;
    this.stream?.getTracks().forEach((t) => t.stop());
    this.stream = null;
    void this.ctx?.close();
    this.ctx = null;
    this.setState('stopped');
  }

  get state(): MicState {
    return this.micState;
  }

  private setState(next: MicState, events?: MicCaptureEvents): void {
    this.micState = next;
    events?.onState?.(next);
  }
}
