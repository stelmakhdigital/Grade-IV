import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createElement } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { setToken } from '../api';
import { AuthProvider } from '../auth';
import { SessionView } from './SessionView';

// Excalidraw не рендерится в jsdom — лёгкий мок (динамический импорт
// подхватит фабрику; холст DesignPanel получает apiRef — мок не ставит).
vi.mock('@excalidraw/excalidraw', () => ({
  Excalidraw: (props: { theme?: string }) =>
    createElement('div', {
      'data-testid': 'excalidraw-mock',
      'data-theme': props.theme ?? 'light',
    }),
}));

/** Минимальный fake WebSocket (совместим с SessionWS). */
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

  constructor(url: string) {
    this.url = url;
    FakeWebSocket.instances.push(this);
    queueMicrotask(() => this.onopen?.());
  }

  send(data: unknown): void {
    this.sent.push(data);
  }

  close(): void {
    this.readyState = 3;
    queueMicrotask(() => this.onclose?.({ code: 1000 }));
  }

  deliver(data: unknown): void {
    this.onmessage?.({ data });
  }
}

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status });
}

const ME = {
  user: { id: 1, email: 'cand@example.com', created_at: '2026-09-14T00:00:00Z' },
  minutes_remaining_s: 3600,
};

const S_ACTIVE = {
  id: 9,
  grade: 'middle',
  stack: 'go',
  stage: 'voice',
  status: 'active',
  duration_limit_s: 3000,
  active_seconds: 10,
  time_left_s: 2990,
  started_at: '2026-09-14T10:00:00Z',
};
const S_FINISHED = { ...S_ACTIVE, status: 'finished', finished_at: '2026-09-14T10:50:00Z' };

const REPORT_MIN = {
  overall: 3.7,
  grade_recommendation: 'грейд подтверждён, есть точки роста',
  criteria: [
    { name: 'Коммуникация (ясность, структура, русский язык)', weight: 0.15, score: 4 },
  ],
  strengths: ['ясно излагал'],
  weaknesses: ['мало примеров'],
  recommendations: ['готовить примеры'],
};

const EVENTS = [
  { seq: 1, ts: '2026-09-14T10:00:05Z', kind: 'session_created', data: { grade: 'middle', stack: 'go' } },
  { seq: 2, ts: '2026-09-14T10:01:00Z', kind: 'user_utterance', data: { text: 'Привет' } },
  { seq: 3, ts: '2026-09-14T10:01:10Z', kind: 'ai_utterance', data: { text: 'Расскажите о себе' } },
];

function mockApi(opts: { session?: unknown; events?: unknown; report?: unknown } = {}) {
  return vi.fn(async (url: string) => {
    if (url.includes('/auth/me')) return json(200, ME);
    if (url.endsWith('/sessions/9')) return json(200, opts.session ?? S_ACTIVE);
    if (url.includes('/events')) return json(200, opts.events ?? EVENTS);
    if (url.includes('/report')) return json(200, opts.report ?? REPORT_MIN);
    if (url.includes('/sessions')) return json(200, []);
    return json(404, {});
  }) as unknown as typeof fetch;
}

function renderSession(fetchMock: unknown) {
  vi.stubGlobal('fetch', fetchMock);
  vi.stubGlobal('WebSocket', FakeWebSocket);
  return render(
    <AuthProvider>
      <SessionView id={9} />
    </AuthProvider>,
  );
}

function flush(): Promise<void> {
  return new Promise((r) => setTimeout(r, 0));
}

beforeEach(() => {
  localStorage.clear();
  setToken('tok');
  FakeWebSocket.instances = [];
});

describe('SessionView (WP-8)', () => {
  it('завершённая сессия — только чтение: запись диалога из /events', async () => {
    renderSession(mockApi({ session: S_FINISHED }));
    expect(await screen.findByText('Кандидат')).toBeInTheDocument();
    expect(screen.getByText('Привет')).toBeInTheDocument();
    expect(screen.getByText('ИИ-интервьюер')).toBeInTheDocument();
    expect(screen.getByText('Расскажите о себе')).toBeInTheDocument();
    expect(screen.queryByTestId('mic-toggle')).toBeNull();
  });

  it('завершённая сессия: отчёт (WP-11) под записью', async () => {
    renderSession(mockApi({ session: S_FINISHED }));
    const report = await screen.findByTestId('report');
    expect(report).toHaveTextContent('Итог: 3.70 / 5');
    expect(screen.getByTestId('report-grade')).toHaveTextContent(
      'грейд подтверждён, есть точки роста',
    );
    expect(screen.getByText('ясно излагал')).toBeInTheDocument();
  });

  it('активная сессия: таймер, живой транскрипт, «ИИ говорит», finish', async () => {
    renderSession(mockApi({ session: S_ACTIVE }));
    await waitFor(() => expect(FakeWebSocket.instances).toHaveLength(1));
    const fake = FakeWebSocket.instances[0];
    expect(fake.url).toContain('/ws/session/9');
    await flush();

    // таймер от сервера
    fake.deliver(JSON.stringify({ type: 'timer', remaining_s: 2950 }));
    expect(await screen.findByTestId('timer')).toHaveTextContent('49:10');

    // живой транскрипт
    fake.deliver(JSON.stringify({ type: 'transcript', who: 'user', text: 'Говорю' }));
    expect(await screen.findByText('Говорю')).toBeInTheDocument();
    fake.deliver(JSON.stringify({ type: 'transcript', who: 'ai', text: 'Отвечаю' }));
    expect(await screen.findByText('Отвечаю')).toBeInTheDocument();

    // ai_text — подписи ИИ
    fake.deliver(JSON.stringify({ type: 'ai_text', text: 'Подпись' }));
    expect(await screen.findByTestId('ai-say')).toHaveTextContent('Подпись');

    // finish → ui-событие в WS
    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'Завершить интервью' }));
    expect(JSON.parse(String(fake.sent[0]))).toEqual({
      type: 'ui',
      name: 'finish',
    });
  }, 10000);

  it('микрофон без доступа к device — статус «нет доступа»', async () => {
    renderSession(mockApi({ session: S_ACTIVE }));
    await waitFor(() => expect(FakeWebSocket.instances).toHaveLength(1));
    await flush();
    const user = userEvent.setup();
    // jsdom: navigator.mediaDevices отсутствует → catch → denied
    await user.click(screen.getByTestId('mic-toggle'));
    expect(await screen.findByText('Нет доступа к микрофону.')).toBeInTheDocument();
  }, 10000);

  it('стадия livecode: панель Live-Code, запуск → POST /runs', async () => {
    const S_LIVECODE = { ...S_ACTIVE, stage: 'livecode' };
    const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
      if (url.includes('/auth/me')) return json(200, ME);
      if (url.endsWith('/sessions/9')) return json(200, S_LIVECODE);
      if (init?.method === 'POST' && url.endsWith('/runs')) {
        return json(200, { exit_code: 0, stdout: '', stderr: '', duration_ms: 10, passed: true, tests: [] });
      }
      if (url.includes('/events')) return json(200, []);
      if (url.includes('/sessions')) return json(200, []);
      return json(404, {});
    });
    renderSession(fetchMock);
    await waitFor(() => expect(FakeWebSocket.instances).toHaveLength(1));
    const fake = FakeWebSocket.instances[0];
    await flush();
    // WS сообщает стадию + задачу
    // WS сообщает стадию + задачу (файлы задачи из банка)
    fake.deliver(JSON.stringify({
      type: 'stage', name: 'livecode',
      task: {
        id: 'go-rev', title: 'Разворот строки', statement: 'Разверните строку',
        files: { 'solution.go': 'package task\n' },
      },
    }));
    expect(await screen.findByTestId('task-title')).toHaveTextContent('Разворот строки');
    // редактор (Monaco) в jsdom не работает — панель должна отрендериться
    // с дефолтным редактором; клик по «Запустить тесты»:
    const user = userEvent.setup();
    await user.click(screen.getByTestId('run-tests'));
    await waitFor(() => {
      const call = (fetchMock.mock.calls as unknown[][]).find(
        (c) => String(c[0]).endsWith('/runs'),
      );
      expect(call).toBeDefined();
    });
    const call = (fetchMock.mock.calls as unknown[][]).find((c) => String(c[0]).endsWith('/runs'));
    const body = JSON.parse((call?.[1] as RequestInit).body as string);
    expect(body).toEqual({
      files: { 'solution.go': 'package task\n' },
      action: 'test',
      task_id: 'go-rev',
    });
  }, 15000);

  it('стадия design: панель System Design с палитрой и холстом', async () => {
    const S_DESIGN = { ...S_ACTIVE, stage: 'design' };
    const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
      if (url.includes('/auth/me')) return json(200, ME);
      if (url.endsWith('/sessions/9')) return json(200, S_DESIGN);
      if (init?.method === 'PUT' && url.endsWith('/whiteboard')) {
        return json(200, { saved: true, structure: { blocks: [], links: 0 } });
      }
      if (url.includes('/events')) return json(200, []);
      if (url.includes('/sessions')) return json(200, []);
      return json(404, {});
    });
    renderSession(fetchMock);
    await waitFor(() => expect(FakeWebSocket.instances).toHaveLength(1));
    const fake = FakeWebSocket.instances[0];
    await flush();
    fake.deliver(JSON.stringify({ type: 'stage', name: 'design', task: null }));
    expect(await screen.findByTestId('design-panel')).toBeInTheDocument();
    // палитра 12 блоков
    expect(screen.getByRole('button', { name: 'Load Balancer' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Monitoring' })).toBeInTheDocument();
    // холст (мок Excalidraw)
    expect(await screen.findByTestId('excalidraw-mock')).toBeInTheDocument();
    // ИИ-оценка (ai_text) на стадии design → блок «Оценка ИИ»
    fake.deliver(JSON.stringify({ type: 'ai_text', text: 'Схема: хорошо учтён кэш.' }));
    expect(await screen.findByTestId('design-review')).toHaveTextContent(
      'Схема: хорошо учтён кэш',
    );
  }, 15000);

  it('stage_action: «К Live-Code» шлёт ui-событие', async () => {
    renderSession(mockApi({ session: S_ACTIVE }));
    await waitFor(() => expect(FakeWebSocket.instances).toHaveLength(1));
    const fake = FakeWebSocket.instances[0];
    await flush();
    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'К Live-Code' }));
    expect(JSON.parse(String(fake.sent[0]))).toEqual({
      type: 'ui',
      name: 'stage_action',
      payload: { stage: 'livecode' },
    });
  }, 10000);
});
