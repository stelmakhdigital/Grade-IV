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

/** Фиктивное возвращаемое useVoiceSession для сценариев mic-dbg/мьюта/записи. */
function fakeVoiceSession(
  micDbg: Record<string, unknown> | null,
  opts: {
    mic?: string;
    error?: string | null;
    lines?: Array<Record<string, unknown>>;
    recActive?: boolean;
    recSegs?: string[];
    recPartial?: string;
    recSince?: number | null;
    recCollapsed?: boolean;
    recNote?: string | null;
    speaking?: boolean;
  } = {},
): Record<string, unknown> {
  return {
    session: S_ACTIVE,
    loadError: null,
    lines: opts.lines ?? [],
    stage: 'voice',
    runReview: '',
    designReview: '',
    task: null,
    remainingS: 2990,
    lastAiText: '',
    mic: opts.mic ?? 'running',
    micDbg,
    speaking: opts.speaking ?? false,
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
    recActive: opts.recActive ?? false,
    recSegs: opts.recSegs ?? [],
    recPartial: opts.recPartial ?? '',
    recTotalSpeechMs: opts.recSince != null ? 3000 : 0,
    recSince: opts.recSince ?? null,
    recCollapsed: opts.recCollapsed ?? false,
    setRecCollapsed: vi.fn(),
    recNote: opts.recNote ?? null,
    finishRecording: vi.fn(),
    clearRecording: vi.fn(),
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

// --- Voice UX (FR-S8): история новые-сверху + автоскролл + окно записи

describe('Voice UX: история новые-сверху + режим записи (FR-S8)', () => {
  it('рендер: последние сообщения СВЕРХУ (live-строки), interim — первая', () => {
    hookMock.fake = fakeVoiceSession(null, {
      lines: [
        { who: 'user', text: 'Первый вопрос кандидата' },
        { who: 'ai', text: 'Ответ ИИ' },
        { who: 'user', text: 'Новая реплика', interim: true },
      ],
    });
    render(
      <AuthProvider>
        <SessionView id={9} />
      </AuthProvider>,
    );
    const items = Array.from(document.querySelectorAll('[data-testid="lines"] > li'));
    expect(items).toHaveLength(3);
    // Новые сверху: последняя (interim) — первый li.
    expect(items[0]).toHaveTextContent('Новая реплика');
    expect(items[0].className).toContain('interim');
    expect(items[1]).toHaveTextContent('Ответ ИИ');
    expect(items[2]).toHaveTextContent('Первый вопрос кандидата');
    cleanup();
  });

  it('REST-история завершённой сессии — тот же порядок (новое сверху)', async () => {
    // EVENTS: user «Привет» → ai «Расскажите о себе» (последнее).
    renderSession(mockApi({ session: S_FINISHED }));
    await screen.findByText('Кандидат');
    const items = Array.from(document.querySelectorAll('[data-testid="lines"] > li'));
    expect(items.length).toBeGreaterThanOrEqual(2);
    expect(items[0]).toHaveTextContent('Расскажите о себе'); // последнее событие — сверху
  });

  it('автоскролл: новое сообщение → scrollTo(0) если пользователь у верхнего края', async () => {
    const scrollToSpy = vi.fn();
    // jsdom: scrollTo отсутствует — ставим шпион на прототип (до рендера).
    (Element.prototype as unknown as { scrollTo: unknown }).scrollTo = scrollToSpy;
    renderSession(mockApi({ session: S_ACTIVE }));
    const fake = await waitFor(() => {
      expect(FakeWebSocket.instances).toHaveLength(1);
      return FakeWebSocket.instances[0]!;
    });
    await flush();
    // Новое сообщение — контейнер у верхнего края (scrollTop=0 < 80) → скролл к началу.
    fake.deliver(JSON.stringify({ type: 'transcript', who: 'ai', text: 'Новое сообщение' }));
    await screen.findByText('Новое сообщение');
    expect(scrollToSpy).toHaveBeenCalledWith({ top: 0 });
    // Пользователь прокрутил вниз (в старые сообщения) — скролл не трогаем.
    const before = scrollToSpy.mock.calls.length;
    const ol = document.querySelector('[data-testid="lines"]') as HTMLOListElement;
    ol.scrollTop = 200; // > 80 — пользователь читает старые
    fake.deliver(JSON.stringify({ type: 'transcript', who: 'ai', text: 'Ещё сообщение' }));
    await screen.findByText('Ещё сообщение');
    expect(scrollToSpy.mock.calls.length).toBe(before);
  });

  it('окно записи: рендер (статус/текст), сворачивание, «недостаточно речи»', async () => {
    hookMock.fake = fakeVoiceSession(null, {
      recActive: true,
      recSegs: ['Привет, меня'],
      recPartial: 'зовут',
      recSince: Date.now() - 45_000,
    });
    render(
      <AuthProvider>
        <SessionView id={9} />
      </AuthProvider>,
    );
    expect(screen.getByTestId('rec-window')).toBeInTheDocument();
    expect(screen.getByTestId('rec-status')).toHaveTextContent('Запись 00:45');
    expect(screen.getByTestId('rec-text')).toHaveTextContent('Привет, меня зовут');
    cleanup();
    // Свёрнутое окно: текст и кнопка отправки скрыты, кнопка — «Развернуть».
    hookMock.fake = fakeVoiceSession(null, {
      recActive: true,
      recSegs: ['Привет, меня'],
      recPartial: 'зовут',
      recSince: Date.now() - 45_000,
      recCollapsed: true,
    });
    render(
      <AuthProvider>
        <SessionView id={9} />
      </AuthProvider>,
    );
    expect(screen.getByTestId('rec-window')).toBeInTheDocument();
    expect(screen.queryByTestId('rec-text')).toBeNull();
    expect(screen.queryByTestId('rec-send')).toBeNull();
    expect(screen.getByTestId('rec-collapse')).toHaveTextContent('Развернуть');
    cleanup();

    // «Недостаточно речи» — заметка после сброса окна.
    hookMock.fake = fakeVoiceSession(null, {
      recNote: 'Недостаточно речи для отправки (минимум ~1.5 с).',
    });
    render(
      <AuthProvider>
        <SessionView id={9} />
      </AuthProvider>,
    );
    expect(screen.getByTestId('rec-note')).toHaveTextContent('Недостаточно речи');
    expect(screen.queryByTestId('rec-window')).toBeNull();
  });

  // --- E2E (реальный хук + fake-микрофон): сегменты НЕ становятся ответами,
  // выключение микрофона → ОДНО utterance-сообщение со склеенным текстом.

  class RecWorkletNode {
    port: { onmessage: ((e: { data: unknown }) => void) | null } = { onmessage: null };
    connect = vi.fn();
    disconnect = vi.fn();
  }
  class RecAudioContext {
    sampleRate = 48000;
    state = 'running';
    audioWorklet = { addModule: vi.fn(async () => undefined) };
    destination = {};
    createMediaStreamSource = () => ({ connect: vi.fn() });
    createGain = () => ({ gain: { value: 1 }, connect: vi.fn() });
    createScriptProcessor = () => ({ onaudioprocess: null, connect: vi.fn(), disconnect: vi.fn() });
    close = vi.fn(async () => undefined);
  }

  function stubMicEnv(): void {
    const track = {
      enabled: true,
      readyState: 'live',
      muted: false,
      label: 'Fake Mic',
      stop: vi.fn(),
      onended: null as null | (() => void),
    };
    const stream = { getTracks: () => [track], getAudioTracks: () => [track] };
    vi.stubGlobal('navigator', { mediaDevices: { getUserMedia: vi.fn(async () => stream) } });
    vi.stubGlobal('AudioContext', RecAudioContext);
    vi.stubGlobal('AudioWorkletNode', RecWorkletNode);
    (URL as unknown as { createObjectURL: (b: Blob) => string }).createObjectURL = () => 'blob:fake';
  }

  it('кнопка мика: «Отправить» во время записи, «Ожидание ИИ…» (disabled) пока ИИ говорит', async () => {
    // Запись активна (мик включён) — кнопка «Отправить».
    hookMock.fake = fakeVoiceSession(null, {
      mic: 'running',
      recActive: true,
      recSegs: ['Привет'],
      recSince: Date.now() - 30_000,
    });
    render(
      <AuthProvider>
        <SessionView id={9} />
      </AuthProvider>,
    );
    expect(screen.getByTestId('mic-toggle')).toHaveTextContent('Отправить');
    expect(screen.getByTestId('mic-toggle')).not.toBeDisabled();
    // «Очистить буфер» — есть и активна (буфер не пуст).
    expect(screen.getByTestId('rec-clear')).toHaveTextContent('Очистить буфер');
    expect(screen.getByTestId('rec-clear')).not.toBeDisabled();
    // Пустой буфер — очистка недоступна.
    cleanup();
    hookMock.fake = fakeVoiceSession(null, { mic: 'running', recActive: true });
    render(
      <AuthProvider>
        <SessionView id={9} />
      </AuthProvider>,
    );
    expect(screen.getByTestId('rec-clear')).toBeDisabled();
    // Мик выключен, ИИ говорит — «Ожидание ИИ…», недоступно (не перебиваем).
    cleanup();
    hookMock.fake = fakeVoiceSession(null, { mic: 'stopped', speaking: true });
    render(
      <AuthProvider>
        <SessionView id={9} />
      </AuthProvider>,
    );
    expect(screen.getByTestId('mic-toggle')).toHaveTextContent('Ожидание ИИ…');
    expect(screen.getByTestId('mic-toggle')).toBeDisabled();
    // Мик выключен, ИИ молчит — «Включить микрофон», доступно.
    cleanup();
    hookMock.fake = fakeVoiceSession(null, { mic: 'stopped' });
    render(
      <AuthProvider>
        <SessionView id={9} />
      </AuthProvider>,
    );
    expect(screen.getByTestId('mic-toggle')).toHaveTextContent('Включить микрофон');
    expect(screen.getByTestId('mic-toggle')).not.toBeDisabled();
  });

  it('режим записи (E2E): сегменты не становятся user-строками и не запускают AI-ход; выключение мика → одно utterance со склеенным текстом', async () => {
    stubMicEnv();
    renderSession(mockApi({ session: S_ACTIVE }));
    const fake = await waitFor(() => {
      expect(FakeWebSocket.instances).toHaveLength(1);
      return FakeWebSocket.instances[0]!;
    });
    await flush();
    const user = userEvent.setup();

    // Включили микрофон → recording on + окно записи.
    await user.click(screen.getByTestId('mic-toggle'));
    expect(await screen.findByTestId('rec-window')).toBeInTheDocument();
    expect(fake.sent.some((m) => JSON.parse(String(m))['name'] === 'recording' && JSON.parse(String(m))['payload']['on'] === true)).toBe(true);

    // Распознавание налёт: partial → окно (не строка), сегменты → окно.
    fake.deliver(JSON.stringify({ type: 'stt_partial', text: 'Привет' }));
    expect(await screen.findByTestId('rec-text')).toHaveTextContent('Привет');
    fake.deliver(JSON.stringify({ type: 'stt_segment', text: 'Привет, меня', speech_ms: 2000, total_speech_ms: 2000 }));
    await screen.findByText(/Привет, меня/);
    fake.deliver(JSON.stringify({ type: 'stt_segment', text: 'зовут Артём', speech_ms: 1000, total_speech_ms: 3000 }));
    expect(await screen.findByTestId('rec-text')).toHaveTextContent('Привет, меня зовут Артём');
    // Ни один сегмент не стал user-строкой.
    expect(document.querySelectorAll('.line.user')).toHaveLength(0);

    // Выключили микрофон → recording off + ОДНО utterance со склеенным текстом.
    await user.click(screen.getByTestId('mic-toggle'));
    expect(screen.queryByTestId('rec-window')).toBeNull();
    const utterances = fake.sent
      .map((m) => JSON.parse(String(m)) as { name?: string; payload?: { text?: string } })
      .filter((m) => m.name === 'utterance');
    expect(utterances).toHaveLength(1);
    expect(utterances[0].payload?.text).toBe('Привет, меня зовут Артём');
    // recording off ушёл (до utterance — WS-порядок).
    const offIdx = fake.sent.findIndex((m) => JSON.parse(String(m))['name'] === 'recording' && JSON.parse(String(m))['payload']['on'] === false);
    const uttIdx = fake.sent.findIndex((m) => JSON.parse(String(m))['name'] === 'utterance');
    expect(offIdx).toBeGreaterThan(-1);
    expect(offIdx).toBeLessThan(uttIdx);

    // AI-ответ (transcript user — как из сервера) — одна user-строка, сверху.
    fake.deliver(JSON.stringify({ type: 'transcript', who: 'user', text: 'Привет, меня зовут Артём' }));
    await screen.findByText('Привет, меня зовут Артём', { selector: '.line-text' });
    expect(document.querySelectorAll('.line.user')).toHaveLength(1);
    const first = document.querySelector('[data-testid="lines"] > li');
    expect(first).toHaveTextContent('Привет, меня зовут Артём');
  }, 15000);

  it('«недостаточно речи» (E2E): запись < 1.5 с речи → utterance НЕ отправляется, заметка', async () => {
    stubMicEnv();
    renderSession(mockApi({ session: S_ACTIVE }));
    const fake = await waitFor(() => {
      expect(FakeWebSocket.instances).toHaveLength(1);
      return FakeWebSocket.instances[0]!;
    });
    await flush();
    const user = userEvent.setup();
    await user.click(screen.getByTestId('mic-toggle'));
    expect(await screen.findByTestId('rec-window')).toBeInTheDocument();
    // Короткая реплика: total_speech_ms 500 < 1500.
    fake.deliver(JSON.stringify({ type: 'stt_segment', text: 'М', speech_ms: 500, total_speech_ms: 500 }));
    await screen.findByText(/М/);
    await user.click(screen.getByTestId('mic-toggle'));
    // utterance не отправлен, recording off — ушёл, заметка «недостаточно речи».
    const utterances = fake.sent
      .map((m) => JSON.parse(String(m)) as { name?: string })
      .filter((m) => m.name === 'utterance');
    expect(utterances).toHaveLength(0);
    expect(await screen.findByTestId('rec-note')).toHaveTextContent('Недостаточно речи');
  }, 15000);
});
