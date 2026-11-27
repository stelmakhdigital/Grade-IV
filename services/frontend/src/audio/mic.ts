/**
 * Микрофон кандидата (WP-8, ADR-002): getUserMedia → AudioContext →
 * AudioWorklet (ресэмплинг до 16 кГц) → чанки PCM16 ~250 мс → WS.
 * Worklet-скрипт встраивается как Blob-URL — отдельный файл не нужен.
 */
import { resampleToPcm16, TARGET_RATE } from './resample';
import { WORKLET_CODE } from './mic-worklet';
import { chunkRms, SilenceDetector, TTS_TAIL_GRACE_MS } from './mic-silence';
import { micDebugEnabled, type MicDebugInfo } from './mic-debug';
import { micUnsupportedReason } from './mic-support';

// Ре-экспорты: mic.ts остаётся единой точкой импорта захвата (`from './mic'`
// работали тесты, SessionView и useVoiceSession до разложения — продолжают).
export * from './mic-worklet';
export * from './mic-silence';
export * from './mic-debug';
export * from './mic-support';

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

// Цепочка надёжности захвата (инцидент 2026-10-08): 3 с без чанков —
// следующий шаг: worklet → fallback (ScriptProcessor) → полная повторная
// инициализация (getUserMedia заново → снова worklet). Не более MAX_REINIT
// полных пересозданий подряд; счётчик сбрасывается первым чанком любого пути.
export const MAX_REINIT = 2;
export const NO_DATA_MS = 3000;

export class MicCapture {
  private ctx: AudioContext | null = null;
  private stream: MediaStream | null = null;
  private node: AudioWorkletNode | null = null;
  private inputRate = 0;
  private micState: MicState = 'idle';
  private noDataTimer: number | null = null;
  private spNode: ScriptProcessorNode | null = null; // резервный захват
  private silence: SilenceDetector | null = null; // детектор «молчащего» микрофона
  private ttsSpeaking = false; // ИИ говорит (TTS) — эхо в мике, детектор off
  private ttsStoppedAt: number | null = null; // конец TTS (Date.now) — grace TTS_TAIL_GRACE_MS
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
  private ctxStateHandler: ((ev: Event) => void) | null = null; // M4: statechange-listener (suspended→resume)

  async start(events: MicCaptureEvents): Promise<void> {
    if (this.micState === 'running' || this.micState === 'muted') return;
    this.events = events;
    if (import.meta.env.DEV) {
      // Dev-инструмент (скриншоты/ручная диагностика): доступ к инстансу
      // захвата из консоли — window.__gradeMic. В prod-сборке отсутствует.
      (window as unknown as Record<string, unknown>).__gradeMic = this;
    }
    // Быстрый путь (toggle = mute, FR-S8): поток жив (soft stop) — просто
    // снимаем мьют (track.enabled=true), без getUserMedia (< 10 мс).
    if (this.micState === 'stopped' && this.isStreamAlive()) {
      this.stream!.getAudioTracks().forEach((t) => {
        t.enabled = true;
      });
      this.armNoDataTimeout();
      this.setState('running', events);
      return;
    }
    await this.initCapture();
  }

  // Поток жив: track существует и не завершён (device removed/changed —
  // readyState 'ended'; тогда toggle идёт через полную инициализацию).
  // getAudioTracks с фолбэком на getTracks (старые тестовые моки).
  private isStreamAlive(): boolean {
    if (this.stream === null) return false;
    const tracks = typeof this.stream.getAudioTracks === 'function'
      ? this.stream.getAudioTracks()
      : this.stream.getTracks();
    const track = tracks[0];
    return track !== undefined && track.readyState === 'live';
  }

  // Полная инициализация захвата: getUserMedia → AudioContext → AudioWorklet.
  // Используется и при первом старте, и при повторной инициализации.
  private async initCapture(): Promise<void> {
    const events = this.events;
    if (events === null) return;
    let stream: MediaStream;
    const unsupported = micUnsupportedReason();
    if (unsupported !== null) {
      this.setState('denied', events);
      events.onError?.(unsupported);
      throw new Error(unsupported);
    }
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
    // Device changed / removed: track завершён — полная реинициализация
    // (мёртвый поток не оживить мьютом, FR-S8). Лимит — MAX_REINIT.
    stream.getTracks().forEach((t) => {
      t.onended = () => {
        this.handleTrackEnded();
      };
    });
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
    // M1: AudioContext, созданный после `await getUserMedia`, часто остаётся
    // `suspended` (user-gesture-активация теряется) — а AudioWorkletNode в
    // suspended-контексте НИКОГДА не вызывает process (тишина → ложный переход на
    // fallback). Явный resume() из user-gesture-контекста (старт микрофона) —
    // основной (worklet) путь работает сразу. Non-blocking: если resume не
    // сработает (нет активации) — сработает watchdog M4 (fallback/reinit).
    if (typeof ctx.resume === 'function') void ctx.resume(); // M1: resume (guard: jsdom-мок без resume)
    // M4: самовосстановление — AudioContext, подвесившийся mid-session (tab в фон /
    // авто-suspend), worklet замер (нет чанков) и «счётчик речи» застревает.
    // statechange → suspended (мик running): resume (если есть активация) + re-arm
    // no-data watchdog (fallback/reinit, если resume не сработал). Guard: jsdom-мок
    // без addEventListener. Снимается в disposeCapture (this.ctxStateHandler).
    const onCtxState = (): void => {
      if (ctx.state === 'suspended' && this.micState === 'running') {
        if (typeof ctx.resume === 'function') void ctx.resume();
        this.armNoDataTimeout();
      }
    };
    this.ctxStateHandler = onCtxState;
    if (typeof ctx.addEventListener === 'function') {
      ctx.addEventListener('statechange', onCtxState);
    }
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
        // Мьют (soft stop, FR-S8): нулевые чанки не шлём. 'muted' —
        // состояние «микрофон молчит» (детектор): чанки идут (поиск возврата).
        if (this.micState !== 'running' && this.micState !== 'muted') return;
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

  // «Выключить микрофон» (toggle, FR-S8): НЕ рвём поток — только мьют
  // (track.enabled=false) + state 'stopped' + сброс счётчиков отладки.
  // Повторное «включить» — быстрый путь в start() (< 10 мс, без getUserMedia).
  // Полное освобождение (tracks/контекст) — dispose() (конец сессии/пауза).
  stop(): void {
    if (this.micState === 'idle') return;
    if (this.isStreamAlive()) {
      this.softMute();
      return;
    }
    this.dispose();
  }

  // Мягкий мьют: поток/контекст/ноды живут — только track.enabled=false.
  // Аудиообработчик в state 'stopped' чанки не шлёт (нулевые чанки не
  // шлём) — гейт в onmessage-обработчиках обоих путей захвата.
  private softMute(): void {
    this.stream?.getAudioTracks().forEach((t) => {
      t.enabled = false;
    });
    if (this.noDataTimer !== null) {
      window.clearTimeout(this.noDataTimer);
      this.noDataTimer = null;
    }
    this.silence?.reset();
    this.silence = null;
    this.dbgChunks = 0;
    this.dbgRms = 0;
    this.dbgGaps = [];
    this.dbgLastTs = 0;
    this.setState('stopped');
  }

  // Полное освобождение ресурсов (tracks/контекст/ноды/таймеры):
  // конец сессии (unmount), пауза, reinit. Состояние ставим ДО disposeCapture:
  // stop-.tracks() вызывает track 'ended' — обработчик не должен увидеть
  // 'running' и запустить reinit.
  dispose(): void {
    if (this.micState !== 'idle') {
      this.setState('stopped');
    }
    this.disposeCapture();
  }

  // Устройство отключено/сменено между сессиями (track 'ended'): мёртвый
  // поток не оживить мьютом — полная повторная инициализация (FR-S8).
  // Лимит MAX_REINIT защищает от цикла при постоянно отключённом устройстве.
  private handleTrackEnded(): void {
    const events = this.events;
    if (events === null) return;
    if (this.micState !== 'running' && this.micState !== 'muted') return;
    events.onInfo?.('Микрофон: устройство отключено — повторяю инициализацию захвата');
    this.reinitCapture(events);
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
    if (this.ctxStateHandler !== null && this.ctx !== null && typeof this.ctx.removeEventListener === 'function') {
      this.ctx.removeEventListener('statechange', this.ctxStateHandler);
    }
    this.ctxStateHandler = null;
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
    // Тихий микрофон при работающем звуке — нормальное состояние диалога:
    // пока ИИ говорит (TTS → эхо в мик) и TTS_TAIL_GRACE_MS после — детектор
    // не кормим (ложная алерт «Микрофон молчит» на паузе/раздумье, инцидент
    // 2026-10-09).
    if (!this.ttsSpeaking && (this.ttsStoppedAt === null || now - this.ttsStoppedAt >= TTS_TAIL_GRACE_MS)) {
      this.silence?.feed(r, now);
    }
  }

  // TTS-статус от хука (speaking): во время речи ИИ (TTS → эхо в мик) и
  // TTS_TAIL_GRACE_MS после — детектор «молчащего» микрофона не кормим
  // (feedSilence гейтит); тишина — нормальное состояние диалога.
  setTtsSpeaking(speaking: boolean): void {
    if (speaking === this.ttsSpeaking) return;
    this.ttsSpeaking = speaking;
    if (speaking) {
      this.silence?.reset(); // эхо не считается «речью кандидата»
      // Сессия явно жива (ИИ отвечает) — снимаем «молчит» из состояния:
      // иначе пользователь, уже заговоривший, видел бы предупреждение до
      // возврата уровня (onRecover сработает на первом громком чанке).
      if (this.micState === 'muted') {
        this.setState('running', this.events ?? undefined);
      }
    } else {
      this.ttsStoppedAt = Date.now();
    }
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
      // spec-14/D11: раньше захардкоженный `http://${hostname}:8000` — ломался
      // за прокси/HTTPS (mixed content) и молча глушился. Теперь same-origin
      // относительный путь (проксирование /debug — в vite.config.ts), как и
      // остальные вызовы api.
      void fetch('/debug/mic-report', {
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
    if (typeof ctx.resume === 'function') void ctx.resume(); // M1 + guard: jsdom-мок без resume
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
        // Мьют (soft stop, FR-S8): нулевые чанки не шлём.
        if (this.micState !== 'running' && this.micState !== 'muted') {
          e.outputBuffer.getChannelData(0).fill(0);
          return;
        }
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
