/**
 * Вкладка «Шаблоны» (src/views/admin/templates.tsx): список, сохранение
 * (PUT с полным телом шаблона), ошибка загрузки (сообщение, а не пустой экран).
 */
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it } from 'vitest';
import { setToken } from '../../api';
import { AuthProvider } from '../../auth';
import { AdminView } from '../AdminView';

const ME_ADMIN = {
  user: { id: 1, email: 'admin@example.com', created_at: '2026-09-14T00:00:00Z', is_admin: true },
};

const TEMPLATES = [
  {
    id: 1,
    name: 'Backend middle',
    grade: 'middle',
    stack: 'go',
    duration_s: 3600,
    is_default: true,
    blocks: [{ title: 'Теория', focus: 'стек', question_count: 5 }],
  },
];

function renderAdmin() {
  return __api.on({
    '/auth/me': ME_ADMIN,
    // точные методы — раньше общего GET-ключа, чтобы matчить и сохранение
    'POST /admin/templates': () => __api.json(201, TEMPLATES[0]),
    'PUT /admin/templates*': (_url: string, init?: RequestInit) =>
      __api.json(200, { ...TEMPLATES[0], ...(init?.body ? JSON.parse(init.body as string) : {}) }),
    '/admin/templates': TEMPLATES,
  });
}

beforeEach(() => {
  localStorage.clear();
  setToken('tok');
  window.location.hash = '#/admin?tab=templates';
});

describe('Админка: вкладка «Шаблоны»', () => {
  it('загружает список, сохраняет правку PUT-ом и показывает ошибку загрузки', async () => {
    const fetchMock = renderAdmin();
    const user = userEvent.setup();
    const view = render(
      <AuthProvider>
        <AdminView />
      </AuthProvider>,
    );
    await screen.findByText('admin@example.com');

    // 1) загрузка: строка списка с именем, дефолтом и числом блоков
    expect(await screen.findByText('Backend middle')).toBeInTheDocument();
    expect(screen.getByText('да')).toBeInTheDocument();
    expect(screen.getByText('1')).toBeInTheDocument();

    // 2) сохранение: PUT /admin/templates/1 с полным телом (без id)
    await user.click(screen.getByRole('button', { name: 'Изменить' }));
    await user.clear(screen.getByLabelText('Имя'));
    await user.type(screen.getByLabelText('Имя'), 'Backend senior');
    await user.click(screen.getByRole('button', { name: 'Сохранить' }));
    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some(
          (c) => c[1]?.method === 'PUT' && String(c[0]).endsWith('/admin/templates/1'),
        ),
      ).toBe(true),
    );
    const put = fetchMock.mock.calls.find(
      (c) => c[1]?.method === 'PUT' && String(c[0]).endsWith('/admin/templates/1'),
    ) as [string, RequestInit];
    expect(JSON.parse(put[1].body as string)).toEqual({
      name: 'Backend senior',
      grade: 'middle',
      stack: 'go',
      duration_s: 3600,
      is_default: true,
      blocks: [{ title: 'Теория', focus: 'стек', question_count: 5 }],
    });

    // 3) ошибка загрузки: 500 → сообщение об ошибке, а не пустой экран
    view.unmount();
    __api.on({ '/admin/templates': __api.failure(500, 'internal', 'шаблоны недоступны') });
    render(
      <AuthProvider>
        <AdminView />
      </AuthProvider>,
    );
    expect(await screen.findByRole('alert')).toHaveTextContent('шаблоны недоступны');
  });
});
