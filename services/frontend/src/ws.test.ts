import { beforeEach, describe, expect, it, vi } from 'vitest';
import { setToken } from './api';
import { SessionWS, type WsMessage } from './ws';

/** Минимальный fake WebSocket (jsdom не реализует WS). */
class FakeWebSocket {
  static instances: FakeWebSocket[] = [];
  static readonly OPEN = 1;
  url: string;
  readyState = 1;
  binaryType = '';
  onopen: (() => void) | null = null;
  onmessage: ((e: { data: unknown }) => void) | null = null;
  onerror: (() => void) | null = null;
  onclose: ((e: { code: number }) => void) | null = null;
  sent: unknown[] = [];
  closed = false;

  constructor(url: string) {
    this.url = url;
    FakeWebSocket.instances.push(this);
    queueMicrotask(() => this.onopen?.());
  }

  send(data: unknown): void {
    this.sent.push(data);
  }

  close(code?: number): void {
    this.closed = true;
    this.readyState = 3;
    queueMicrotask(() => this.onclose?.({ code: code ?? 1000 }));
  }

  deliver(data: unknown): void {
    this.onmessage?.({ data });
  }
}

function flush(): Promise<void> {
  return new Promise((r) => setTimeout(r, 0));
}

beforeEach(() => {
  localStorage.clear();
  setToken('tok');
  FakeWebSocket.instances = [];
  vi.stubGlobal('WebSocket', FakeWebSocket);
});

describe('SessionWS (WP-8)', () => {
  it('URL: /ws/session/{id}?token=… и binaryType=arraybuffer', async () => {
    const ws = new SessionWS();
    ws.connect(42, {
      onMessage: () => {},
      onAudio: () => {},
    });
    await flush();
    const fake = FakeWebSocket.instances[0];
    expect(fake.url).toContain('/ws/session/42?token=tok');
    expect(fake.binaryType).toBe('arraybuffer');
  });

  it('JSON-сообщения: timer/transcript/error доставляются в onMessage', async () => {
    const seen: WsMessage[] = [];
    const ws = new SessionWS();
    ws.connect(1, { onMessage: (m) => seen.push(m), onAudio: () => {} });
    await flush();
    const fake = FakeWebSocket.instances[0];
    fake.deliver(JSON.stringify({ type: 'timer', remaining_s: 123 }));
    fake.deliver(JSON.stringify({ type: 'transcript', who: 'user', text: 'привет' }));
    fake.deliver(JSON.stringify({ type: 'error', code: 'x', msg: 'ошибка' }));
    expect(seen.map((m) => m.type)).toEqual(['timer', 'transcript', 'error']);
  });

  it('бинарный кадр {seq,flags}+PCM → onAudio с pcm и isLast', async () => {
    const audio: { pcm: Int16Array; isLast: boolean }[] = [];
    const ws = new SessionWS();
    ws.connect(1, { onMessage: () => {}, onAudio: (pcm, isLast) => audio.push({ pcm, isLast }) });
    await flush();
    const fake = FakeWebSocket.instances[0];
    const buf = new ArrayBuffer(4 + 4);
    const v = new DataView(buf);
    v.setUint16(0, 3, true);
    v.setUint16(2, 1, true);
    v.setInt16(4, 5, true);
    v.setInt16(6, 6, true);
    fake.deliver(buf);
    expect(audio).toHaveLength(1);
    expect(audio[0].pcm).toEqual(new Int16Array([5, 6]));
    expect(audio[0].isLast).toBe(true);
  });

  it('sendUi / sendPcm — JSON и бинарные кадры в WS', async () => {
    const ws = new SessionWS();
    ws.connect(1, { onMessage: () => {}, onAudio: () => {} });
    await flush();
    const fake = FakeWebSocket.instances[0];
    expect(ws.sendUi('stage_action', { stage: 'livecode' })).toBe(true);
    expect(JSON.parse(String(fake.sent[0]))).toEqual({
      type: 'ui',
      name: 'stage_action',
      payload: { stage: 'livecode' },
    });
    expect(ws.sendPcm(new Int16Array([1, 2]))).toBe(true);
    expect(fake.sent[1]).toBeInstanceOf(ArrayBuffer);
    expect(new Int16Array(fake.sent[1] as ArrayBuffer)).toEqual(new Int16Array([1, 2]));
  });

  it('close(): код 1000, onClose не вызывается (инициатор — клиент)', async () => {
    let closeReason: string | null = null;
    const ws = new SessionWS();
    ws.connect(1, {
      onMessage: () => {},
      onAudio: () => {},
      onClose: (r) => (closeReason = r),
    });
    await flush();
    ws.close();
    await flush();
    expect(FakeWebSocket.instances[0].closed).toBe(true);
    expect(closeReason).toBeNull();
  });

  it('код закрытия 1011 → onClose(aborted)', async () => {
    let closeReason: string | null = null;
    const ws = new SessionWS();
    ws.connect(1, {
      onMessage: () => {},
      onAudio: () => {},
      onClose: (r) => (closeReason = r),
    });
    await flush();
    const fake = FakeWebSocket.instances[0];
    fake.onclose?.({ code: 1011 });
    expect(closeReason).toBe('aborted');
  });
});
