import { cleanup, render, screen, waitFor } from '@testing-library/react';
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

// useVoiceSession: по умолчанию — реальный хук (все остальные сценарии);
// в тестах mic-dbg подменяем возвращаемое значение (в jsdom реальный
// захват микрофона/Web Audio нет, получить micDbg.chunks > 0 нельзя).
const hookMock = vi.hoisted(() => ({
  fake: null as null | Record<string, unknown>,
}));
vi.mock('../hooks/useVoiceSession', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../hooks/useVoiceSession')>();
  return {
    ...actual,
    useVoiceSession: (id: number) =>
      hookMock.fake === null ? actual.useVoiceSession(id) : hookMock.fake,
  };
});

/** Фиктивное возвращаемое useVoiceSession для сценариев mic-dbg/мьюта. */
function fakeVoiceSession(
  micDbg: Record<string, unknown> | null,
  opts: { mic?: string; error?: string | null } = {},
): Record<string, unknown> {
  return {
    session: S_ACTIVE,
    loadError: null,
    lines: [],
    stage: 'voice',
    runReview: '',
    designReview: '',
    task: null,
    remainingS: 2990,
    lastAiText: '',
    mic: opts.mic ?? 'running',
    micDbg,
    speaking: false,
    wsState: 'open',
    error: opts.error ?? null,
    micLevelRef: { current: 0 },
    live: true,
    paused: false,
    pauseBusy: false,
    toggleMic: vi.fn(),
    pause: vi.fn(),
    resume: vi.fn(),
    onStageAction: vi.fn(),
    onFinish: vi.fn(),
  };
}

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

/** fetch-мок с доступным .mock.calls (для ассертов POST-вызовов). */
type FetchMock = {
  (url: string, init?: RequestInit): Promise<Response>;
  mock: { calls: unknown[][] };
};

function mockApi(opts: { session?: unknown; events?: unknown; report?: unknown } = {}): FetchMock {
  return vi.fn(async (url: string, init?: RequestInit) => {
    if (url.includes('/auth/me')) return json(200, ME);
    if (init?.method === 'POST' && url.endsWith('/sessions/9/pause')) {
      return json(200, { ...(opts.session ?? S_ACTIVE), status: 'paused', paused_at: '2026-09-14T10:10:00Z' });
    }
    if (init?.method === 'POST' && url.endsWith('/sessions/9/resume')) {
      return json(200, opts.session ?? S_ACTIVE);
    }
    if (url.endsWith('/sessions/9')) return json(200, opts.session ?? S_ACTIVE);
    if (url.includes('/events')) return json(200, opts.events ?? EVENTS);
    if (url.includes('/report')) return json(200, opts.report ?? REPORT_MIN);
    if (url.includes('/sessions')) return json(200, []);
    return json(404, {});
  }) as unknown as FetchMock;
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
  hookMock.fake = null;
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

  it('стриминговый STT: stt_partial → интерим-строка, final фиксирует её', async () => {
    renderSession(mockApi({ session: S_ACTIVE }));
    await waitFor(() => expect(FakeWebSocket.instances).toHaveLength(1));
    const fake = FakeWebSocket.instances[0];
    await flush();

    // partial: интеримный текст кандидата (обновляется на той же строке).
    fake.deliver(JSON.stringify({ type: 'stt_partial', text: 'Расскажите' }));
    const interim = await screen.findByText('Расскажите');
    expect(interim).toBeInTheDocument();
    expect(interim.closest('.line')?.className).toContain('interim');

    // второй partial — та же строка (обновление, не новая строка).
    fake.deliver(JSON.stringify({ type: 'stt_partial', text: 'Расскажите о себе' }));
    await screen.findByText('Расскажите о себе');
    expect(document.querySelectorAll('.line.user')).toHaveLength(1);

    // final (transcript user) — интерим зафиксирован: одна строка, без interim.
    fake.deliver(JSON.stringify({ type: 'transcript', who: 'user', text: 'Расскажите о себе.' }));
    await screen.findByText('Расскажите о себе.');
    const line = document.querySelector('.line.user');
    expect(document.querySelectorAll('.line.user')).toHaveLength(1);
    expect(line?.className).not.toContain('interim');
    expect(line).toHaveTextContent('Расскажите о себе.');
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

  it('пауза (FR-S7): активная сессия — «Пауза», клик → POST /pause + баннер + «Продолжить»', async () => {
    const fetchMock = mockApi();
    renderSession(fetchMock);
    await waitFor(() => expect(FakeWebSocket.instances).toHaveLength(1));
    const fake = FakeWebSocket.instances[0];
    await flush();

    // до паузы: баннера нет, есть кнопка «Пауза» и нет «Продолжить»
    expect(screen.queryByTestId('paused-banner')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Продолжить' })).toBeNull();
    const user = userEvent.setup();
    await user.click(await screen.findByRole('button', { name: 'Пауза' }));

    // вызов pauseSession: POST /api/v1/sessions/9/pause
    await waitFor(() => {
      const call = (fetchMock.mock.calls as unknown[][]).find((c) =>
        String(c[0]).endsWith('/sessions/9/pause'),
      );
      expect(call).toBeDefined();
      expect((call?.[1] as RequestInit).method).toBe('POST');
    });

    // paused-состояние: баннер, «Продолжить», статуса «пауза» в шапке
    const banner = await screen.findByTestId('paused-banner');
    expect(banner).toHaveTextContent('время не тарифицируется');
    expect(await screen.findByRole('button', { name: 'Продолжить' })).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Пауза' })).toBeNull());
    expect(await screen.findByText('пауза')).toBeInTheDocument();
    // WS не переподключался (один и тот же инстанс), таймер жив
    expect(FakeWebSocket.instances).toHaveLength(1);
    fake.deliver(JSON.stringify({ type: 'timer', remaining_s: 2900 }));
    expect(await screen.findByTestId('timer')).toHaveTextContent('48:20');

    // «Продолжить» → POST /resume, пауза снята
    await user.click(screen.getByRole('button', { name: 'Продолжить' }));
    await waitFor(() => {
      const call = (fetchMock.mock.calls as unknown[][]).find((c) =>
        String(c[0]).endsWith('/sessions/9/resume'),
      );
      expect(call).toBeDefined();
      expect((call?.[1] as RequestInit).method).toBe('POST');
    });
    await waitFor(() => expect(screen.queryByTestId('paused-banner')).toBeNull());
    expect(await screen.findByRole('button', { name: 'Пауза' })).toBeInTheDocument();
  }, 10000);

  it('пауза (FR-S7): перезагрузка страницы на paused-сессии — баннер и «Продолжить»', async () => {
    const S_PAUSED = { ...S_ACTIVE, status: 'paused', paused_at: '2026-09-14T10:10:00Z' };
    renderSession(mockApi({ session: S_PAUSED }));
    await waitFor(() => expect(FakeWebSocket.instances).toHaveLength(1));
    expect(await screen.findByTestId('paused-banner')).toHaveTextContent('время не тарифицируется');
    expect(screen.getByRole('button', { name: 'Продолжить' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Пауза' })).toBeNull();
    // микрофон на паузе не включается: кнопка «Включить микрофон» недоступна
    expect(screen.getByTestId('mic-toggle')).toBeDisabled();
  }, 10000);

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

  it('mic-dbg (dev): строка статистики рендерится при micDbg.chunks > 0', async () => {
    vi.stubEnv('VITE_MIC_DEBUG', 'true');
    vi.stubGlobal('fetch', mockApi());
    hookMock.fake = fakeVoiceSession({
      path: 'worklet',
      ctxState: 'running',
      rate: 16000,
      chunks: 10,
      medGapMs: 250,
      maxRms: 0.5,
    });
    render(
      <AuthProvider>
        <SessionView id={9} />
      </AuthProvider>,
    );
    const dbg = await screen.findByText(/мик\[worklet\]/);
    expect(dbg.className).toContain('mic-dbg');
    expect(dbg).toHaveTextContent('чанков 10');
    expect(dbg).toHaveTextContent('шаг 250 мс');
    expect(dbg).toHaveTextContent('max rms 0.5');
    expect(dbg).toHaveTextContent('ctx running');
    vi.unstubAllEnvs();
  }, 10000);

  it('mic-dbg (dev): строки нет при chunks = 0 или micDbg = null', async () => {
    vi.stubEnv('VITE_MIC_DEBUG', 'true');
    vi.stubGlobal('fetch', mockApi());

    // chunks = 0 — блок не рендерится
    hookMock.fake = fakeVoiceSession({
      path: 'worklet',
      ctxState: 'running',
      rate: 16000,
      chunks: 0,
      medGapMs: 0,
      maxRms: 0,
    });
    render(
      <AuthProvider>
        <SessionView id={9} />
      </AuthProvider>,
    );
    // voice-panel отрендерился (микрофон «включён»), но строки диагностики нет
    expect(await screen.findByText('микрофон: включён')).toBeInTheDocument();
    expect(document.querySelector('.mic-dbg')).toBeNull();
    cleanup();

    // micDbg = null — тоже нет
    hookMock.fake = fakeVoiceSession(null);
    render(
      <AuthProvider>
        <SessionView id={9} />
      </AuthProvider>,
    );
    expect(await screen.findByText('микрофон: включён')).toBeInTheDocument();
    expect(document.querySelector('.mic-dbg')).toBeNull();
    vi.unstubAllEnvs();
  }, 10000);

  it('mic-dbg (dev): без VITE_MIC_DEBUG строки нет даже при chunks > 0', async () => {
    vi.stubGlobal('fetch', mockApi());
    hookMock.fake = fakeVoiceSession({
      path: 'worklet',
      ctxState: 'running',
      rate: 16000,
      chunks: 10,
      medGapMs: 250,
      maxRms: 0.5,
    });
    render(
      <AuthProvider>
        <SessionView id={9} />
      </AuthProvider>,
    );
    expect(await screen.findByText('микрофон: включён')).toBeInTheDocument();
    expect(document.querySelector('.mic-dbg')).toBeNull();
  }, 10000);

  it('muted + error с /audio-debug.html — кликабельные ссылки (самодиагностика)', async () => {
    vi.stubGlobal('fetch', mockApi());
    hookMock.fake = fakeVoiceSession(null, {
      mic: 'muted',
      error:
        'Микрофон молчит: проверьте устройство, мьют и разрешения браузера. ' +
        'Диагностика: откройте /audio-debug.html и проверьте уровень сигнала с микрофона.',
    });
    render(
      <AuthProvider>
        <SessionView id={9} />
      </AuthProvider>,
    );
    // строка ошибки: путь /audio-debug.html — тег <a> с href
    const link = await screen.findByTestId('debug-link');
    expect(link.tagName).toBe('A');
    expect(link).toHaveAttribute('href', '/audio-debug.html');
    expect(link).toHaveTextContent('/audio-debug.html');
    // muted-баннер тоже ссылается на диагностику
    const mutedLink = screen.getByTestId('debug-link-mic-muted');
    expect(mutedLink.tagName).toBe('A');
    expect(mutedLink).toHaveAttribute('href', '/audio-debug.html');
  }, 10000);

  it('error без /audio-debug.html — рендерится обычным текстом, ссылки нет', async () => {
    vi.stubGlobal('fetch', mockApi());
    hookMock.fake = fakeVoiceSession(null, {
      mic: 'denied',
      error: 'Нет доступа к микрофону — разрешите в браузере.',
    });
    render(
      <AuthProvider>
        <SessionView id={9} />
      </AuthProvider>,
    );
    expect(await screen.findByText('Нет доступа к микрофону — разрешите в браузере.')).toBeInTheDocument();
    expect(screen.queryByTestId('debug-link')).toBeNull();
    expect(screen.queryByTestId('debug-link-mic-muted')).toBeNull();
  }, 10000);
});
