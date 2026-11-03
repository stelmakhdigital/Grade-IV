import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi, type Mock } from 'vitest';
import { setToken } from '../api';
import { AuthProvider } from '../auth';
import { AdminView } from './AdminView';

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status });
}

const ME_ADMIN = {
  user: { id: 1, email: 'admin@example.com', created_at: '2026-09-14T00:00:00Z', is_admin: true },
  minutes_remaining_s: 3600,
};

const TRACKS = [
  {
    id: 'backend',
    title: 'Backend (Go/Python)',
    coding: true,
    stacks: ['go', 'python'],
    grades: ['junior', 'middle', 'senior', 'staff'],
    board: {
      title: 'Схема системы',
      review_prompt: 'Оцени полноту и обоснованность решений',
      groups: [{ title: 'Трафик', names: ['Клиент', 'Load Balancer'] }],
      cases: [{ grade: 'middle', title: 'Миграция', statement: 'Спроектируй миграцию', checklist: ['ЛБ', 'БД'] }],
    },
    criteria: [
      { name: 'Глубина', weight: 0.6, hint: 'техническая глубина' },
      { name: 'Коммуникация', weight: 0.4 },
    ],
    grade_confirm: 4.5,
  },
  {
    id: 'sysadmin',
    title: 'Сисадмин',
    coding: false,
    grades: ['junior', 'middle'],
    board: { title: 'Схема сети', review_prompt: 'Оцени полноту схемы', groups: [] },
    criteria: [{ name: 'Порядок', weight: 1 }],
  },
];

interface MockOpts {
  tracks?: unknown;
  createStatus?: number;
  createBody?: unknown;
  deleteStatus?: number;
  deleteBody?: unknown;
}

function mockApi(opts: MockOpts = {}): Mock {
  return vi.fn(async (url: string, init?: RequestInit) => {
    if (url.includes('/auth/me')) return json(200, ME_ADMIN);
    if (url.endsWith('/admin/tracks')) {
      if (init?.method === 'POST') {
        return json(opts.createStatus ?? 201, opts.createBody ?? (init.body ? JSON.parse(init.body as string) : {}));
      }
      return json(200, opts.tracks ?? TRACKS);
    }
    if (url.includes('/admin/tracks/')) {
      if (init?.method === 'DELETE') {
        return json(
          opts.deleteStatus ?? 200,
          opts.deleteBody ?? { ok: true },
        );
      }
    }
    return json(404, { code: 'not_found', message: 'not found' });
  });
}

function renderAdmin(fetchMock: Mock) {
  vi.stubGlobal('fetch', fetchMock);
  return render(
    <AuthProvider>
      <AdminView />
    </AuthProvider>,
  );
}

beforeEach(() => {
  localStorage.clear();
  setToken('tok');
});

describe('AdminView: треки собеседований (spec-18/Ф4)', () => {
  it('загружает и показывает список треков с бейджами типа', async () => {
    const fetchMock = mockApi();
    renderAdmin(fetchMock);
    // вкладка «Треки собеседований»
    const user = userEvent.setup();
    await screen.findByText('admin@example.com');
    await user.click(screen.getByRole('tab', { name: 'Треки собеседований' }));
    expect(await screen.findByTestId('admin-tracks')).toBeInTheDocument();
    expect(screen.getByText('Backend (Go/Python)')).toBeInTheDocument();
    expect(screen.getByText('Сисадмин')).toBeInTheDocument();
    expect(screen.getByText('кодовый')).toBeInTheDocument();
    expect(screen.getByText('некодовый')).toBeInTheDocument();
    // GET /admin/tracks вызван
    expect(fetchMock.mock.calls.some((c) => String(c[0]).includes('/admin/tracks'))).toBe(true);
  });

  it('создание трека: POST с body {track}', async () => {
    const fetchMock = mockApi();
    renderAdmin(fetchMock);
    const user = userEvent.setup();
    await screen.findByText('admin@example.com');
    await user.click(screen.getByRole('tab', { name: 'Треки собеседований' }));
    await screen.findByTestId('admin-tracks');

    await user.click(screen.getByTestId('admin-track-new'));
    await user.type(screen.getByTestId('admin-track-title'), 'Аналитик данных');
    await user.type(screen.getByLabelText('ID'), 'data-analyst');
    await user.click(screen.getByTestId('admin-track-save'));

    await waitForPost(fetchMock);
    const post = fetchMock.mock.calls.find((c) => c[1]?.method === 'POST' && String(c[0]).endsWith('/admin/tracks'));
    expect(post).toBeDefined();
    const body = JSON.parse((post as unknown as [string, RequestInit])[1].body as string);
    expect(body.track.id).toBe('data-analyst');
    expect(body.track.title).toBe('Аналитик данных');
    expect(body.track.coding).toBe(true);
  });

  it('удаление используемого трека: 409 → сообщение «используется сессиями»', async () => {
    const fetchMock = mockApi({ deleteStatus: 409, deleteBody: { code: 'track_in_use', message: 'track exists in sessions' } });
    renderAdmin(fetchMock);
    const user = userEvent.setup();
    await screen.findByText('admin@example.com');
    await user.click(screen.getByRole('tab', { name: 'Треки собеседований' }));
    await screen.findByTestId('admin-tracks');

    // выбрать трек в селекте → открывается редактор
    await user.selectOptions(screen.getByTestId('admin-track-select'), 'backend');
    expect(await screen.findByTestId('admin-track-delete')).toBeInTheDocument();
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true);
    await user.click(screen.getByTestId('admin-track-delete'));
    expect(confirmSpy).toHaveBeenCalled();

    expect(await screen.findByText(/используется сессиями/)).toBeInTheDocument();
    const del = fetchMock.mock.calls.find((c) => c[1]?.method === 'DELETE' && String(c[0]).includes('/admin/tracks/backend'));
    expect(del).toBeDefined();
  });
});

async function waitForPost(fetchMock: Mock): Promise<void> {
  await waitFor(() =>
    expect(fetchMock.mock.calls.some((c) => c[1]?.method === 'POST' && String(c[0]).endsWith('/admin/tracks'))).toBe(true),
  );
}
