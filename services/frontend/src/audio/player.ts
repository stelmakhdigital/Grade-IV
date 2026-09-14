/**
 * PCM-плеер TTS-кадров (WP-8): очередь AudioBuffer (16 кГц mono),
 * планирование воспроизведения в AudioContext; isSpeaking() —
 * индикатор «ИИ говорит» (turn-taking UI, SRS §8).
 */
import { pcmDurationS, TARGET_RATE } from './resample';

export class PcmPlayer {
  private ctx: AudioContext | null = null;
  private queue: Int16Array[] = [];
  private nextStartAt = 0; // ctx.time следующего буфера
  private playing = false;
  private speaking = false;
  private checkTimer: number | null = null;

  /** Добавляет кадр PCM16; стартует воспроизведение, если не идёт. */
  playChunk(pcm: Int16Array): void {
    if (pcm.length === 0) return;
    this.queue.push(pcm);
    if (!this.playing) this.startPlayback();
  }

  /** Последняя реплика ИИ завершена (end-флаг) — доигрываем очередь. */
  markUtteranceEnd(): void {
    // Очистка «говорит» произойдёт по таймеру, когда очередь опустеет.
  }

  isSpeaking(): boolean {
    return this.speaking;
  }

  /** Полная остановка (сброс очереди). */
  stop(): void {
    this.queue = [];
    this.playing = false;
    this.speaking = false;
    if (this.checkTimer !== null) {
      window.clearTimeout(this.checkTimer);
      this.checkTimer = null;
    }
  }

  dispose(): void {
    this.stop();
    void this.ctx?.close();
    this.ctx = null;
  }

  private ensureCtx(): AudioContext {
    if (this.ctx === null) {
      const Ctx = window.AudioContext ?? (window as any).webkitAudioContext;
      this.ctx = new Ctx({ sampleRate: TARGET_RATE });
      this.nextStartAt = 0;
    }
    if (this.ctx.state === 'suspended') {
      void this.ctx.resume();
    }
    return this.ctx;
  }

  private startPlayback(): void {
    const ctx = this.ensureCtx();
    this.playing = true;
    this.speaking = true;
    this.nextStartAt = Math.max(ctx.currentTime + 0.02, this.nextStartAt);
    this.pump(ctx);
    this.scheduleCheck();
  }

  private pump(ctx: AudioContext): void {
    while (this.queue.length > 0) {
      const pcm = this.queue[0];
      const buffer = ctx.createBuffer(1, pcm.length, TARGET_RATE);
      buffer.copyToChannel(new Float32Array(this.floatFromPcm(pcm)), 0);
      const src = ctx.createBufferSource();
      src.buffer = buffer;
      src.connect(ctx.destination);
      src.start(this.nextStartAt);
      this.nextStartAt += pcmDurationS(pcm);
      this.queue.shift();
    }
  }

  private floatFromPcm(pcm: Int16Array): Float32Array {
    const f = new Float32Array(pcm.length);
    for (let i = 0; i < pcm.length; i++) {
      f[i] = pcm[i] / 32768;
    }
    return f;
  }

  private scheduleCheck(): void {
    if (this.checkTimer !== null) return;
    this.checkTimer = window.setTimeout(() => {
      this.checkTimer = null;
      const ctx = this.ctx;
      if (ctx === null) return;
      const queueEndsAt = this.nextStartAt;
      if (this.queue.length === 0 && ctx.currentTime >= queueEndsAt) {
        this.speaking = false;
        this.playing = false;
        return;
      }
      this.scheduleCheck();
    }, 200);
  }
}
