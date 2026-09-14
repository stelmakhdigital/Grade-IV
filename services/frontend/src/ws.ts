/**
 * WS-клиент сессии (WP-8, протокол ARCHITECTURE.md §4.2, ADR-001).
 * S→C: JSON (stage/timer/ai_text/transcript/run_result/report_ready/error)
 *      + бинарные кадры TTS {seq u16 LE, flags u16 LE} + PCM16;
 * C→S: бинарные PCM16-кадры (микрофон) + {"type":"ui","name","payload"}.
 */
import { getToken } from './api';
import { parseTtsFrame } from './audio/resample';

export interface StageTask {
  id?: string;
  title?: string;
  statement?: string;
  /** Полный набор файлов задачи (включая тесты) — Live-Code (WP-9). */
  files?: Record<string, string>;
}

export type WsMessage =
  | { type: 'stage'; name: string; task?: StageTask | null }
  | { type: 'timer'; remaining_s: number }
  | { type: 'ai_text'; text: string }
  | { type: 'transcript'; who: 'user' | 'ai'; text: string }
  | { type: 'run_result'; [k: string]: unknown }
  | { type: 'report_ready'; [k: string]: unknown }
  | { type: 'error'; code: string; msg: string };

export interface WsEvents {
  onMessage: (m: WsMessage) => void;
  onAudio: (pcm: Int16Array, isLast: boolean) => void;
  onOpen?: () => void;
  onClose?: (reason: 'closed' | 'error' | 'aborted') => void;
}

export class SessionWS {
  private ws: WebSocket | null = null;
  private closedByUs = false;

  /** Подключение: /ws/session/{id}?token= (JWT из localStorage). */
  static buildUrl(id: number): string {
    const proto = window.location.protocol === 'https:' ? 'wss' : 'ws';
    return `${proto}://${window.location.host}/ws/session/${id}?token=${encodeURIComponent(getToken() ?? '')}`;
  }

  connect(id: number, events: WsEvents): void {
    if (this.ws !== null) return;
    const ws = new WebSocket(SessionWS.buildUrl(id));
    ws.binaryType = 'arraybuffer';
    this.ws = ws;
    ws.onopen = () => events.onOpen?.();
    ws.onmessage = (e: MessageEvent) => {
      if (e.data instanceof ArrayBuffer) {
        const frame = parseTtsFrame(e.data);
        if (frame !== null) {
          events.onAudio(frame.pcm, frame.isLast);
        }
        return;
      }
      try {
        const m = JSON.parse(String(e.data)) as WsMessage;
        events.onMessage(m);
      } catch {
        // не-JSON текст — игнорируем (протокол строго JSON/бинарные)
      }
    };
    ws.onerror = () => {
      events.onClose?.('error');
    };
    ws.onclose = (e: CloseEvent) => {
      this.ws = null;
      if (this.closedByUs) return;
      // 4001/1008 — разлогин/недоступно; 1011 — aborted сервером.
      events.onClose?.(e.code === 1011 ? 'aborted' : 'closed');
    };
  }

  /** UI-событие кандидата: stage_action/finish/utterance (текстовый режим). */
  sendUi(name: string, payload?: unknown): boolean {
    if (this.ws === null || this.ws.readyState !== WebSocket.OPEN) return false;
    const body: { type: string; name: string; payload?: unknown } = { type: 'ui', name };
    if (payload !== undefined) body.payload = payload;
    this.ws.send(JSON.stringify(body));
    return true;
  }

  /** PCM16-кадр микрофона (бинарный, без заголовка). */
  sendPcm(pcm: Int16Array): boolean {
    if (this.ws === null || this.ws.readyState !== WebSocket.OPEN) return false;
    this.ws.send(pcm.buffer.slice(pcm.byteOffset, pcm.byteOffset + pcm.byteLength));
    return true;
  }

  get open(): boolean {
    return this.ws !== null && this.ws.readyState === WebSocket.OPEN;
  }

  close(): void {
    this.closedByUs = true;
    this.ws?.close(1000, 'client');
    this.ws = null;
  }
}
