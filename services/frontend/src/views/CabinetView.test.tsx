import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi, type Mock } from 'vitest';
import { setToken } from '../api';
import { AuthProvider } from '../auth';
import { CabinetView } from './CabinetView';

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status });
}

const ME = {
  user: { id: 1, email: 'cand@example.com', created_at: '2026-09-14T00:00:00Z' },
  minutes_remaining_s: 3600,
};

const S_FINISHED = {
  id: 1,
  grade: 'middle',
  stack: 'go',
  stage: 'report',
  status: 'finished',
  duration_limit_s: 3000,
  active_seconds: 3000,
  time_left_s: 0,
  started_at: '2026-09-14T10:00:00Z',
  finished_at: '2026-09-14T10:50:00Z',
};
const S_ACTIVE = {
  id: 2,
  grade: 'junior',
  stack: 'python',
  stage: 'voice',
  status: 'active',
  duration_limit_s: 2700,
  active_seconds: 60,
  time_left_s: 2640,
  started_at: '2026-09-14T11:00:00Z',
};

function mockApi(opts: { sessions?: unknown; create?: unknown; me?: unknown } = {}): Mock {
  return vi.fn(async (url: string, init?: RequestInit) => {
    if (url.includes('/auth/me')) return json(200, opts.me ?? ME);
    if (init?.method === 'POST' && url.endsWith('/sessions')) {
      return json(201, opts.create ?? S_ACTIVE);
    }
    if (url.includes('/sessions')) return json(200, opts.sessions ?? []);
    return json(404, {});
  });
}

function renderCabinet(fetchMock: Mock) {
  vi.stubGlobal('fetch', fetchMock);
  return render(
    <AuthProvider>
      <CabinetView />
    </AuthProvider>,
  );
}

beforeEach(() => {
  localStorage.clear();
  setToken('tok');
});

describe('CabinetView (WP-7)', () => {
  it('показывает email и минуты; история со статусами и действиями', async () => {
    const fetchMock = mockApi({ sessions: [S_ACTIVE, S_FINISHED] });
    renderCabinet(fetchMock);
    expect(await screen.findByTestId('user-email')).toBeInTheDocument();
    expect(screen.getByTestId('minutes')).toHaveTextContent('60');
    expect(screen.getByText('активна')).toBeInTheDocument();
    expect(screen.getByText('завершена')).toBeInTheDocument();
    // завершённая → «Транскрипт», активная → «Продолжить»
    const links = screen.getAllByRole('link');
    expect(links.some((l) => l.textContent === 'Транскрипт')).toBe(true);
    expect(links.some((l) => l.textContent === 'Продолжить')).toBe(true);
  });

  it('пустая история — подсказка', async () => {
    renderCabinet(mockApi({ sessions: [] }));
    expect(await screen.findByText(/Сессий пока нет/)).toBeInTheDocument();
  });

  it('старт: создание сессии и переход в #/sessions/{id}', async () => {
    const fetchMock = mockApi({ sessions: [], create: S_ACTIVE });
    renderCabinet(fetchMock);
    await screen.findByTestId('user-email');
    const user = userEvent.setup();
    await user.selectOptions(screen.getByLabelText('Грейды'), 'junior');
    await user.selectOptions(screen.getByLabelText('Стек'), 'python');
    await user.click(screen.getByTestId('start-session'));
    expect(window.location.hash).toBe('#/sessions/2');
    expect(JSON.parse((fetchMock.mock.calls.find((c) => c[1]?.method === 'POST')?.[1] as RequestInit).body as string)).toEqual({
      grade: 'junior',
      stack: 'python',
    });
  });

  it('недостаток минут: кнопка старта заблокирована с предупреждением', async () => {
    const fetchMock = mockApi({
      sessions: [],
      me: { user: ME.user, minutes_remaining_s: 1800 }, // 30 мин
    });
    renderCabinet(fetchMock);
    await screen.findByTestId('user-email');
    const user = userEvent.setup();
    await user.selectOptions(screen.getByLabelText('Грейды'), 'staff'); // 75 мин
    expect(await screen.findByText(/Не хватает минут/)).toBeInTheDocument();
    expect(screen.getByTestId('start-session')).toBeDisabled();
  });
});
