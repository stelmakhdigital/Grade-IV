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
  /** Неблокирующее info-сообщение (напр., переход на fallback-захват). */
  onInfo?: (msg: string) => void;
}

export type MicState = 'idle' | 'running' | 'stopped' | 'denied' | 'muted';

// «Молчащий» микрофон: rms чанка < SILENCE_RMS непрерывно ≥ SILENCE_MS —
// устройство заглушено/отключено или вход на мьюте (ревью 2026-10-05: сервер
// видел rms 2→0 в сессии 13). Детектор чистый (время передаётся аргументом) — тесты
// без DOM.
export const SILENCE_RMS = 0.005;
export const SILENCE_MS = 5000;
// Цепочка надёжности захвата (инцидент 2026-10-08): 3 с без чанков —
// следующий шаг: worklet → fallback (ScriptProcessor) → полная повторная
// инициализация (getUserMedia заново → снова worklet). Не более MAX_REINIT
// полных пересозданий подряд; счётчик сбрасывается первым чанком любого пути.
export const MAX_REINIT = 2;
export const NO_DATA_MS = 3000;

export function chunkRms(pcm: Int16Array): number {
  let s = 0;
  for (let i = 0; i < pcm.length; i++) {
    const v = pcm[i] / 32768;
    s += v * v;
  }
  return Math.sqrt(s / Math.max(1, pcm.length));
}

export interface SilenceEvents {
  /** Тишина ≥ SILENCE_MS (однажды за период). */
  onMuted: () => void;
  /** Уровень вернулся (rms ≥ SILENCE_RMS) после «молчания». */
  onRecover: () => void;
}

export class SilenceDetector {
  private silentSince: number | null = null;
  private muted = false;

  constructor(private readonly ev: SilenceEvents) {}

  reset(): void {
    this.silentSince = null;
    this.muted = false;
  }

  isMuted(): boolean {
    return this.muted;
  }

  feed(rms: number, now: number): void {
    if (rms >= SILENCE_RMS) {
      this.silentSince = null;
      if (this.muted) {
        this.muted = false;
        this.ev.onRecover();
      }
      return;
    }
    if (this.muted) return; // предупреждение за период — одно
    if (this.silentSince === null) {
      this.silentSince = now;
    } else if (now - this.silentSince >= SILENCE_MS) {
      this.muted = true;
      this.ev.onMuted();
    }
  }
}

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

// Dev-режим диагностики микрофона: live-отчёты на /debug/mic-report каждые 10 с.
// Включение: VITE_MIC_DEBUG=1 (vite env) — в production-сборке выключено.
// Функция (не константа): значение читается при вызове — тесты стабятся через vi.stubEnv.
export function micDebugEnabled(): boolean {
  return (import.meta.env.VITE_MIC_DEBUG ?? '') === 'true';
}

export interface MicDebugInfo {
  path: 'worklet' | 'fallback';
  ctxState: AudioContextState | null;
  rate: number;
  chunks: number;
  medGapMs: number;
  maxRms: number;
}

export class MicCapture {
  private ctx: AudioContext | null = null;
  private stream: MediaStream | null = null;
  private node: AudioWorkletNode | null = null;
  private inputRate = 0;
  private micState: MicState = 'idle';
  private noDataTimer: number | null = null;
  private spNode: ScriptProcessorNode | null = null; // резервный захват
  private silence: SilenceDetector | null = null; // детектор «молчащего» микрофона
  // Живая диагностика (dev): статистика чанков + автоотчёт /debug/mic-report.
  private dbgChunks = 0;
  private dbgRms = 0;
  private dbgGaps: number[] = [];
  private dbgLastTs = 0;
  private dbgTimer: number | null = null;
  private pathRef: 'worklet' | 'fallback' = 'worklet';
  private events: MicCaptureEvents | null = null; // живые события (для reinit)
  private reinitCount = 0; // полных пересозданий подряд (сброс — первый чанок)
  private reiniting = false; // защита от дубля пересоздания

  async start(events: MicCaptureEvents): Promise<void> {
    if (this.micState === 'running' || this.micState === 'muted') return;
    this.events = events;
    if (import.meta.env.DEV) {
      // Dev-инструмент (скриншоты/ручная диагностика): доступ к инстансу
      // захвата из консоли — window.__gradeMic. В prod-сборке отсутствует.
      (window as unknown as Record<string, unknown>).__gradeMic = this;
    }
    await this.initCapture();
  }

  // Полная инициализация захвата: getUserMedia → AudioContext → AudioWorklet.
  // Используется и при первом старте, и при повторной инициализации.
  private async initCapture(): Promise<void> {
    const events = this.events;
    if (events === null) return;
    let stream: MediaStream;
    try {
      stream = await navigator.mediaDevices.getUserMedia({
        audio: {
          channelCount: 1,
          // 16 кГц — желательно, но браузер может вернуть системную частоту;
          // ресэмплинг в worklet работает в обоих случаях.
          sampleRate: TARGET_RATE,
          // Базовая эхо-защита (barge-in): микрофон слушается и во время речи
          // ИИ (серверный VAD отделяет голос кандидата) — подавляем эхо
          // динамика→микрофон на уровне браузера.
          echoCancellation: true,
          noiseSuppression: true,
        },
      });
    } catch (err) {
      // Нет доступа (включая повторные попытки): state 'denied' + конкретика.
      this.setState('denied', events);
      events.onError?.(
        'Нет доступа к микрофону: браузер отклонил запрос. Разрешите доступ в ' +
          'настройках браузера (значок замка/микрофона рядом с адресом) или ' +
          'откройте диагностику /audio-debug.html.',
      );
      throw err;
    }
    this.stream = stream;
    const Ctx = window.AudioContext ?? (window as any).webkitAudioContext;
    let ctx: AudioContext;
    try {
      ctx = new Ctx();
    } catch (err) {
      stream.getTracks().forEach((t) => t.stop());
      this.stream = null;
      events.onError?.('Web Audio недоступен — обновите браузер (Chrome/Edge последних лет).');
      throw err;
    }
    this.ctx = ctx;
    this.inputRate = ctx.sampleRate;
    try {
      await ctx.audioWorklet.addModule(
        URL.createObjectURL(new Blob([WORKLET_CODE], { type: 'application/javascript' })),
      );
    } catch (err) {
      // Диагностика: без AudioWorklet микрофон «включён», но данных нет.
      stream.getTracks().forEach((t) => t.stop());
      this.stream = null;
      void ctx.close();
      this.ctx = null;
      events.onError?.(
        'Микрофон не работает: браузер не загрузил аудио-модуль (AudioWorklet). ' +
          'Откройте в актуальном Chrome или Edge и повторите.',
      );
      throw err;
    }
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
    // Диагностика: 3 с без чанков — AudioWorklet молчит (в некоторых
    // средах процессор не расписывается). Тогда — резервный путь:
    // legacy ScriptProcessor (он работает там, где worklet не гонится);
    // если и он молчит — полная повторная инициализация (см. reinitCapture).
    this.armNoDataTimeout();
    // Детектор «молчащего» микрофона (rms чанков, любой путь захвата).
    this.silence = new SilenceDetector({
      onMuted: () => {
        this.setState('muted', events);
        events.onError?.(
          'Микрофон молчит: проверьте устройство, мьют и разрешения браузера. ' +
            'Диагностика: откройте /audio-debug.html и проверьте уровень сигнала с микрофона.',
        );
      },
      onRecover: () => this.setState('running', events),
    });
    node.port.onmessage = (e: MessageEvent) => {
      const d = e.data;
      if (d instanceof Int16Array) {
        this.onFirstChunk();
        this.feedSilence(d);
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
    this.pathRef = 'worklet';
    this.startDebugReporter();
    this.setState('running', events);
  }

  stop(): void {
    if (this.micState === 'idle') return;
    this.disposeCapture();
    this.setState('stopped');
  }

  // Освобождение ресурсов захвата (tracks, контекст, ноды, таймеры) без
  // смены состояния — общая часть stop() и повторной инициализации.
  private disposeCapture(): void {
    if (this.noDataTimer !== null) {
      window.clearTimeout(this.noDataTimer);
      this.noDataTimer = null;
    }
    if (this.dbgTimer !== null) {
      window.clearInterval(this.dbgTimer);
      this.dbgTimer = null;
    }
    this.silence?.reset();
    this.silence = null;
    if (this.spNode !== null) {
      this.spNode.onaudioprocess = null;
      this.spNode.disconnect();
      this.spNode = null;
    }
    this.node?.disconnect();
    this.node = null;
    this.stream?.getTracks().forEach((t) => t.stop());
    this.stream = null;
    void this.ctx?.close();
    this.ctx = null;
  }

  get state(): MicState {
    return this.micState;
  }

  // NO_DATA_MS без аудио-чанков: следующий шаг цепочки надёжности —
  // резервный захват (ScriptProcessor) или полная повторная инициализация
  // (если fallback уже активен и лимит MAX_REINIT не исчерпан).
  private armNoDataTimeout(): void {
    if (this.noDataTimer !== null) {
      window.clearTimeout(this.noDataTimer);
    }
    this.noDataTimer = window.setTimeout(() => {
      this.noDataTimer = null;
      const events = this.events;
      if (events === null || this.micState !== 'running') return;
      // Диагностика: почему путь молчит (состояние контекста, устройство).
      const track = typeof this.stream?.getAudioTracks === 'function'
        ? this.stream.getAudioTracks()[0]
        : null;
      console.info('mic-debug', {
        kind: this.spNode === null ? 'worklet-silent' : 'fallback-silent',
        ctxState: this.ctx?.state,
        rate: this.inputRate,
        track: track ? track.label : null,
        muted: track?.muted ?? null,
        enabled: track?.enabled ?? null,
      });
      if (this.spNode === null) {
        this.switchToScriptProcessor(events);
      } else if (this.reinitCount >= MAX_REINIT) {
        // Лимит повторных инициализаций исчерпан — финальная ошибка
        // с конкретной инструкцией (диагностика — audio-debug.html).
        events.onError?.(
          'Микрофон включён, но аудио-данные не приходят: повторные инициализации ' +
            'не помогли. Проверьте устройство, мьют и разрешения браузера; ' +
            'диагностика — /audio-debug.html.',
        );
      } else {
        this.reinitCapture(events);
      }
    }, NO_DATA_MS);
  }

  // Первый чанок от любого пути — снимаем таймер диагностики и сбрасываем
  // счётчик пересозданий (данные идут — цепочка сработала).
  private onFirstChunk(): void {
    if (this.noDataTimer !== null) {
      window.clearTimeout(this.noDataTimer);
      this.noDataTimer = null;
    }
    this.reinitCount = 0;
  }

  // Полная повторная инициализация: worklet и fallback молчали —
  // останавливаем tracks/контекст и повторяем getUserMedia → worklet-путь.
  // UI узнаёт об этом через onInfo (сессия не блокируется); ошибка доступа
  // обрабатывается в initCapture (state 'denied' + onError).
  private reinitCapture(events: MicCaptureEvents): void {
    if (this.reiniting) return;
    this.reiniting = true;
    this.reinitCount++;
    events.onInfo?.(
      `Микрофон: резервный путь молчит — повторяю инициализацию (попытка ${this.reinitCount})`,
    );
    this.disposeCapture();
    void (async () => {
      try {
        await this.initCapture();
      } catch {
        // getUserMedia отклонён: state 'denied' + onError уже сообщены в initCapture.
      } finally {
        this.reiniting = false;
      }
    })();
  }

  // Кормим детектор тишины (только когда микрофон «включён», включая
  // состояние «молчит» — там ищем возвращение уровня). Плюс живая диагностика.
  private feedSilence(pcm: Int16Array): void {
    if (this.micState !== 'running' && this.micState !== 'muted') return;
    const now = Date.now();
    if (this.dbgLastTs > 0) {
      const gap = now - this.dbgLastTs;
      if (gap >= 0 && gap < 30000) {
        this.dbgGaps.push(gap);
        if (this.dbgGaps.length > 8) this.dbgGaps.shift();
      }
    }
    this.dbgLastTs = now;
    this.dbgChunks++;
    const r = chunkRms(pcm);
    if (r > this.dbgRms) this.dbgRms = r;
    this.silence?.feed(r, now);
  }

  /** Dev: статистика захвата для UI/отчёта. */
  debugInfo(): MicDebugInfo {
    const sorted = [...this.dbgGaps].sort((a, b) => a - b);
    const med = sorted.length > 0 ? sorted[Math.floor(sorted.length / 2)] : 0;
    return {
      path: this.pathRef,
      ctxState: this.ctx?.state ?? null,
      rate: this.inputRate,
      chunks: this.dbgChunks,
      medGapMs: med,
      maxRms: Math.round(this.dbgRms * 10000) / 10000,
    };
  }

  /** Dev: раз в 10 с — снапшот захвата на /debug/mic-report (тихо, не критичен). */
  private startDebugReporter(): void {
    if (!micDebugEnabled()) return;
    if (this.dbgTimer !== null) return;
    this.dbgTimer = window.setInterval(() => {
      const info = this.debugInfo();
      void fetch(`http://${location.hostname}:8000/debug/mic-report`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ kind: 'mic-capture-live', time: new Date().toISOString(), ua: navigator.userAgent, ...info }),
      }).catch(() => {});
    }, 10000);
  }

  // Резервный захват (legacy ScriptProcessorNode, main thread): работает в
  // средах, где AudioWorklet не расписывается. Чанки — по каждому callback
  // (~107 мс @48 кГц → ~36 мс @16 кГц после ресемплинга — VAD не требует
  // фиксированного размера кадра).
  private switchToScriptProcessor(events: MicCaptureEvents): void {
    const ctx = this.ctx;
    const stream = this.stream;
    if (!ctx || !stream || this.micState !== 'running') return;
    try {
      // Выключаем worklet, чтобы не было двух потоков из одного микрофона.
      this.node?.disconnect();
      this.node = null;
      const source = ctx.createMediaStreamSource(stream);
      const sp = ctx.createScriptProcessor(4096, 1, 1);
      let winSamples = Math.max(1, Math.round(ctx.sampleRate * 0.1));
      let sum = 0;
      let n = 0;
      sp.onaudioprocess = (e: AudioProcessingEvent) => {
        const input = e.inputBuffer.getChannelData(0);
        for (let i = 0; i < input.length; i++) {
          sum += input[i] * input[i];
        }
        n += input.length;
        if (n >= winSamples) {
          events.onLevel?.(Math.sqrt(sum / n));
          sum = 0;
          n = 0;
          winSamples = Math.max(1, Math.round(ctx.sampleRate * 0.1));
        }
        e.outputBuffer.getChannelData(0).fill(0); // без звука в динамики
        const pcm = resampleToPcm16(input, ctx.sampleRate);
        if (pcm.length > 0) {
          this.onFirstChunk();
          this.feedSilence(pcm);
          events.onChunk(pcm);
        }
      };
      const silence = ctx.createGain();
      silence.gain.value = 0;
      source.connect(sp);
      sp.connect(silence);
      silence.connect(ctx.destination);
      this.spNode = sp;
      this.pathRef = 'fallback';
      console.info('mic-debug', {
        kind: 'fallback-on',
        ctxState: ctx.state,
        rate: ctx.sampleRate,
        track: typeof this.stream?.getAudioTracks === 'function'
          ? (this.stream.getAudioTracks()[0]?.label ?? null)
          : null,
      });
      // Переход на fallback — информировать UI (не блокирующее info).
      events.onInfo?.(
        'Микрофон: основной путь (AudioWorklet) молчит, включён резервный захват',
      );
      // Если и резервный путь молчит — повторная инициализация (см. armNoDataTimeout).
      this.armNoDataTimeout();
    } catch {
      events.onError?.(
        'Микрофон не работает: ни AudioWorklet, ни резервный захват не дали данных. Обновите браузер (Chrome/Edge).',
      );
    }
  }

  private setState(next: MicState, events?: MicCaptureEvents): void {
    this.micState = next;
    events?.onState?.(next);
  }
}
