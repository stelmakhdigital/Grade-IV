import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { setToken } from '../api';
import { AuthProvider } from '../auth';
import { AdminView } from './AdminView';

const ME_ADMIN = {
  user: { id: 1, email: 'admin@example.com', created_at: '2026-09-14T00:00:00Z', is_admin: true },
};

const TRACKS = [
  {
    id: 'backend',
    title: 'Backend (Go)',
    practice: { runtime: 'go' },
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
    practice: { runtime: 'shell' },
    grades: ['junior', 'middle'],
    board: { title: 'Схема сети', review_prompt: 'Оцени полноту схемы', groups: [] },
    criteria: [{ name: 'Порядок', weight: 1 }],
  },
];

interface MockOpts {
  tracks?: unknown;
  createStatus?: number;
  createBody?: unknown;
  updateStatus?: number;
  updateBody?: unknown;
  generateStatus?: number;
  generateBody?: unknown;
  deleteStatus?: number;
  deleteBody?: unknown;
}

/** Маршруты admin-редактора ролей (spec-18/22): POST/PUT/DELETE/generate. */
function adminRoutes(opts: MockOpts = {}): Record<string, unknown> {
  /** Ответ-эхо: сервер возвращает присланную роль (или override теста). */
  const echo = (status: number, body: unknown, init?: RequestInit): Response =>
    __api.json(status, body ?? (init?.body ? JSON.parse(init.body as string) : {}));
  return {
    '/auth/me': ME_ADMIN,
    // generate раньше идёт по точному пути — 'POST /admin/tracks' матчит и его,
    // поэтому порядок в таблице важен.
    'POST /admin/tracks/generate': () =>
      __api.json(opts.generateStatus ?? 200, opts.generateBody ?? {
        track: {
          id: 'data-engineer',
          title: 'Data-инженер',
          description: 'Строит пайплайны данных.',
          practice: { runtime: 'python' },
          grades: ['junior', 'middle'],
          board: { title: 'Схема пайплайна', review_prompt: 'Оцени полноту', groups: [{ title: 'Источники', names: ['API'] }] },
          criteria: [{ name: 'Моделирование', weight: 0.5 }, { name: 'Коммуникация', weight: 0.5 }],
        },
      }),
    'POST /admin/tracks': (_url: string, init?: RequestInit) =>
      echo(opts.createStatus ?? 201, opts.createBody, init),
    'PUT /admin/tracks*': (_url: string, init?: RequestInit) =>
      echo(opts.updateStatus ?? 200, opts.updateBody, init),
    'DELETE /admin/tracks*': () => __api.json(opts.deleteStatus ?? 200, opts.deleteBody ?? { ok: true }),
    // Список ролей — любой другой метод на /admin/tracks.
    '/admin/tracks': opts.tracks ?? TRACKS,
  };
}

/** Рендер админки; возвращает fetch-mock — ассерты по URL, методу и телу. */
function renderAdmin(opts: MockOpts = {}) {
  const fetchMock = __api.on(adminRoutes(opts));
  render(
    <AuthProvider>
      <AdminView />
    </AuthProvider>,
  );
  return fetchMock;
}

beforeEach(() => {
  localStorage.clear();
  setToken('tok');
  // вкладка админки живёт в адресе — иначе соседний тест оставляет ?tab=…
  window.location.hash = '#/admin';
});

describe('AdminView: роли собеседований (spec-18/22)', () => {
  it('загружает и показывает список ролей с бейджами практики (spec-22)', async () => {
    const fetchMock = renderAdmin();
    const user = userEvent.setup();
    await screen.findByText('admin@example.com');
    await user.click(screen.getByRole('tab', { name: 'Роли собеседований' }));
    expect(await screen.findByTestId('admin-tracks')).toBeInTheDocument();
    expect(screen.getByText('Backend (Go)')).toBeInTheDocument();
    expect(screen.getByText('Сисадмин')).toBeInTheDocument();
    // бейджи практики по рантайму роли
    expect(screen.getByText('практика: Go')).toBeInTheDocument();
    expect(screen.getByText('практика: bash')).toBeInTheDocument();
    // GET /admin/tracks вызван
    expect(fetchMock.mock.calls.some((c) => String(c[0]).includes('/admin/tracks'))).toBe(true);
  });

  it('создание трека: POST с body {track}', async () => {
    const fetchMock = renderAdmin();
    const user = userEvent.setup();
    await screen.findByText('admin@example.com');
    await user.click(screen.getByRole('tab', { name: 'Роли собеседований' }));
    await screen.findByTestId('admin-tracks');

    await user.click(screen.getByTestId('admin-track-new'));
    await user.type(screen.getByTestId('admin-track-title'), 'Аналитик данных');
    await user.type(screen.getByLabelText('ID'), 'data-analyst');
    await user.click(screen.getByTestId('admin-track-practice'));
    await user.click(screen.getByTestId('admin-track-save'));

    await waitForPost(fetchMock);
    const post = fetchMock.mock.calls.find((c) => c[1]?.method === 'POST' && String(c[0]).endsWith('/admin/tracks'));
    expect(post).toBeDefined();
    const body = JSON.parse((post as unknown as [string, RequestInit])[1].body as string);
    expect(body.track.id).toBe('data-analyst');
    expect(body.track.title).toBe('Аналитик данных');
    expect(body.track.practice).toEqual({ runtime: 'go' });
  });

  it('spec-22: ИИ-заполнение новой роли — форма предзаполнена черновиком', async () => {
    const fetchMock = renderAdmin();
    const user = userEvent.setup();
    await screen.findByText('admin@example.com');
    await user.click(screen.getByRole('tab', { name: 'Роли собеседований' }));
    await screen.findByTestId('admin-tracks');
    await user.click(screen.getByTestId('admin-track-new'));
    // ИИ-блок виден у новой роли
    expect(screen.getByTestId('admin-track-ai-desc')).toBeInTheDocument();
    await user.type(screen.getByTestId('admin-track-ai-desc'), 'Инженер по данным на Python: пайплайны и дашборды');
    await user.click(screen.getByTestId('admin-track-ai-fill'));
    // POST /admin/tracks/generate ушёл с описанием
    const gen = fetchMock.mock.calls.find((c) => String(c[0]).endsWith('/admin/tracks/generate'));
    expect(gen).toBeDefined();
    expect(JSON.parse((gen as unknown as [string, RequestInit])[1].body as string).description).toContain('Python');
    // Форма предзаполнена черновиком, ИИ-блок скрыт
    expect(await screen.findByDisplayValue('Data-инженер')).toBeInTheDocument();
    expect((screen.getByTestId('admin-track-practice') as HTMLInputElement).checked).toBe(true);
    expect(screen.getByTestId('admin-track-runtime')).toHaveValue('python');
    expect(screen.queryByTestId('admin-track-ai-desc')).not.toBeInTheDocument();
    expect(screen.getByDisplayValue('data-engineer')).toBeEnabled(); // id правится перед сохранением
  });

  it('палитра-превью: чипы групп как на доске кандидата', async () => {
    renderAdmin();
    const user = userEvent.setup();
    await screen.findByText('admin@example.com');
    await user.click(screen.getByRole('tab', { name: 'Роли собеседований' }));
    await screen.findByTestId('admin-tracks');
    await user.selectOptions(screen.getByTestId('admin-track-select'), 'backend');
    const preview = await screen.findByTestId('admin-track-palette-preview');
    expect(preview).toBeInTheDocument();
    expect(screen.getByText('Трафик')).toBeInTheDocument();
    expect(screen.getByText('Клиент')).toBeInTheDocument();
    expect(screen.getByText('Load Balancer')).toBeInTheDocument();
  });

  it('spec-22: практика — чекбокс + рантайм, в payload уходит practice:{runtime}', async () => {
    const fetchMock = renderAdmin();
    const user = userEvent.setup();
    await screen.findByText('admin@example.com');
    await user.click(screen.getByRole('tab', { name: 'Роли собеседований' }));
    await screen.findByTestId('admin-tracks');

    await user.click(screen.getByTestId('admin-track-new'));
    await user.type(screen.getByTestId('admin-track-title'), 'Сисадмин сети');
    await user.type(screen.getByLabelText('ID'), 'netops');
    const practice = screen.getByTestId('admin-track-practice') as HTMLInputElement;
    await user.click(practice);
    expect(practice.checked).toBe(true);
    await user.selectOptions(screen.getByTestId('admin-track-runtime'), 'shell');
    await user.click(screen.getByTestId('admin-track-save'));
    await waitForPost(fetchMock);
    const post = fetchMock.mock.calls.find((c) => c[1]?.method === 'POST' && String(c[0]).endsWith('/admin/tracks'));
    const body = JSON.parse((post as unknown as [string, RequestInit])[1].body as string);
    expect(body.track.practice).toEqual({ runtime: 'shell' });
  });

  it('issue #7: практика заблокирована у роли с незавершёнными сессиями', async () => {
    renderAdmin({
      tracks: [
        { ...TRACKS[0], active_sessions: 2 },
        { ...TRACKS[1], active_sessions: 0 },
      ],
    });
    const user = userEvent.setup();
    await screen.findByText('admin@example.com');
    await user.click(screen.getByRole('tab', { name: 'Роли собеседований' }));
    await screen.findByTestId('admin-tracks');

    await user.selectOptions(screen.getByTestId('admin-track-select'), 'backend');
    expect(screen.getByTestId('admin-track-practice')).toBeDisabled();
    expect(screen.getByTestId('admin-track-runtime')).toBeDisabled();
    expect(screen.getByTestId('admin-track-practice-locked')).toBeInTheDocument();

    // Роль без незавершённых сессий — практика доступна.
    await user.selectOptions(screen.getByTestId('admin-track-select'), 'sysadmin');
    expect(screen.getByTestId('admin-track-practice')).toBeEnabled();
    expect(screen.queryByTestId('admin-track-practice-locked')).not.toBeInTheDocument();
  });

  it('issue #7: 409 на сохранение — в форме честная причина', async () => {
    renderAdmin({ updateStatus: 409, updateBody: { code: 'track_in_use', message: 'идут сессии' } });
    const user = userEvent.setup();
    await screen.findByText('admin@example.com');
    await user.click(screen.getByRole('tab', { name: 'Роли собеседований' }));
    await screen.findByTestId('admin-tracks');

    await user.selectOptions(screen.getByTestId('admin-track-select'), 'backend');
    await user.type(screen.getByTestId('admin-track-title'), ' +');
    await user.click(screen.getByTestId('admin-track-save'));
    expect(await screen.findByRole('alert')).toHaveTextContent('незавершённые сессии');
  });

  it('drag-n-drop: перестановка групп за ручку', async () => {
    renderAdmin();
    const user = userEvent.setup();
    await screen.findByText('admin@example.com');
    await user.click(screen.getByRole('tab', { name: 'Роли собеседований' }));
    await screen.findByTestId('admin-tracks');
    await user.click(screen.getByTestId('admin-track-new'));
    await user.type(screen.getByTestId('admin-track-title'), 'Трек DnD');
    // две группы: A и B
    const groupRows = () => screen.getAllByTestId(/^admin-track-group-row-/);
    await user.type(within(groupRows()[0]).getByLabelText('Группа'), 'Альфа');
    await user.click(screen.getByRole('button', { name: '+ Группа' }));
    await user.type(within(groupRows()[1]).getByLabelText('Группа'), 'Бета');
    // drag строки 0 на строку 1 → Бета должна стать первой
    const r0 = screen.getByTestId('admin-track-group-row-0');
    const r1 = screen.getByTestId('admin-track-group-row-1');
    fireEvent.dragStart(r0.querySelector('.drag-handle') as HTMLElement, { dataTransfer: { setData: vi.fn() } });
    fireEvent.dragOver(r1, { dataTransfer: { getData: () => '0' } });
    fireEvent.drop(r1, { dataTransfer: { getData: () => '0' } });
    // порядок: первая строка теперь «Бета»
    expect(within(screen.getByTestId('admin-track-group-row-0')).getByLabelText('Группа')).toHaveValue('Бета');
    expect(within(screen.getByTestId('admin-track-group-row-1')).getByLabelText('Группа')).toHaveValue('Альфа');
  });

  it('drag-n-drop: перенос блока (чипа) между группами в превью', async () => {
    renderAdmin();
    const user = userEvent.setup();
    await screen.findByText('admin@example.com');
    await user.click(screen.getByRole('tab', { name: 'Роли собеседований' }));
    await screen.findByTestId('admin-tracks');
    await user.click(screen.getByTestId('admin-track-new'));
    await user.type(screen.getByTestId('admin-track-title'), 'Трек чипов');
    const groupRows = () => screen.getAllByTestId(/^admin-track-group-row-/);
    await user.type(within(groupRows()[0]).getByLabelText('Группа'), 'Сети');
    await user.type(within(groupRows()[0]).getByLabelText('Имена'), 'Файрвол');
    await user.click(screen.getByRole('button', { name: '+ Группа' }));
    await user.type(within(groupRows()[1]).getByLabelText('Группа'), 'Серверы');
    // превью: чип «Файрвол» из группы 0 перетаскиваем в группу 1
    const chip = screen.getByTestId('admin-track-chip-0-0');
    const target = screen.getByTestId('admin-track-palette-group-1');
    fireEvent.dragStart(chip, { dataTransfer: { setData: vi.fn() } });
    fireEvent.dragOver(target, { dataTransfer: { getData: () => '0' } });
    fireEvent.drop(target, { dataTransfer: { getData: () => '0' } });
    // имена ушли: у группы 0 пусто, у группы 1 — Файрвол
    expect(within(screen.getByTestId('admin-track-group-row-0')).getByLabelText('Имена')).toHaveValue('');
    expect(within(screen.getByTestId('admin-track-group-row-1')).getByLabelText('Имена')).toHaveValue('Файрвол');
  });

  it('удаление используемого трека: 409 → сообщение «используется сессиями»', async () => {
    const fetchMock = renderAdmin({ deleteStatus: 409, deleteBody: { code: 'track_in_use', message: 'track exists in sessions' } });
    const user = userEvent.setup();
    await screen.findByText('admin@example.com');
    await user.click(screen.getByRole('tab', { name: 'Роли собеседований' }));
    await screen.findByTestId('admin-tracks');

    // выбрать трек в селекте → открывается редактор
    await user.selectOptions(screen.getByTestId('admin-track-select'), 'backend');
    expect(await screen.findByTestId('admin-track-delete')).toBeInTheDocument();
    // ui-review 2026-11-25: вместо window.confirm — модалка проекта (её
    // до сих пор порезал бы iframe с sandbox, и в тестах она недоступна)
    await user.click(screen.getByTestId('admin-track-delete'));
    await user.click(await screen.findByTestId('confirm-yes'));

    expect(await screen.findByText(/используется сессиями/)).toBeInTheDocument();
    const del = fetchMock.mock.calls.find((c) => c[1]?.method === 'DELETE' && String(c[0]).includes('/admin/tracks/backend'));
    expect(del).toBeDefined();
  });

  it('порядок критериев меняется кнопками ↑/↓ (клавиатурный путь, ui-review 2026-11-27)', async () => {
    renderAdmin();
    const user = userEvent.setup();
    await screen.findByText('admin@example.com');
    await user.click(screen.getByRole('tab', { name: 'Роли собеседований' }));
    await screen.findByTestId('admin-tracks');
    await user.click(screen.getByTestId('admin-track-new'));

    const table = await screen.findByTestId('admin-track-criteria');
    const names = () => Array.from(table.querySelectorAll<HTMLInputElement>('tbody tr td:first-child input'));
    await user.type(names()[0], 'Альфа');
    await user.click(screen.getByRole('button', { name: '+ Критерий' }));
    await user.type(names()[1], 'Бета');
    expect(names().map((i) => i.value)).toEqual(['Альфа', 'Бета']);

    await user.click(within(table).getByRole('button', { name: 'Критерий «Альфа» ниже' }));
    expect(names().map((i) => i.value)).toEqual(['Бета', 'Альфа']);
    await user.click(within(table).getByRole('button', { name: 'Критерий «Альфа» выше' }));
    expect(names().map((i) => i.value)).toEqual(['Альфа', 'Бета']);
  });

  it('вкладки админки: стрелки переводят выбор, раздел живёт в адресе', async () => {
    window.location.hash = '#/admin';
    renderAdmin();
    const user = userEvent.setup();
    await screen.findByText('admin@example.com');
    screen.getByRole('tab', { name: 'Шаблоны' }).focus();
    await user.keyboard('{ArrowRight}');
    expect(screen.getByRole('tab', { name: 'Начала разговора' })).toHaveAttribute('aria-selected', 'true');
    expect(window.location.hash).toContain('tab=openings');
    await user.keyboard('{Home}');
    expect(screen.getByRole('tab', { name: 'Шаблоны' })).toHaveAttribute('aria-selected', 'true');
  });

});

async function waitForPost(fetchMock: ReturnType<typeof __api.on>): Promise<void> {
  await waitFor(() =>
    expect(fetchMock.mock.calls.some((c) => c[1]?.method === 'POST' && String(c[0]).endsWith('/admin/tracks'))).toBe(true),
  );
}
