/**
 * Вкладка «Начала разговора» (src/views/admin/openings.tsx): список,
 * сохранение всех строк одним PUT, ошибка загрузки.
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

const OPENINGS = [
  { grade: '*', prompt: 'Всем привет' },
  { grade: 'middle', prompt: 'Привет' },
];

function renderAdmin() {
  return __api.on({
    '/auth/me': ME_ADMIN,
    // сохранение — PUT списком целиком; точный метод раньше общего GET-ключа
    'PUT /admin/openings': (_url: string, init?: RequestInit) =>
      __api.json(200, init?.body ? JSON.parse(init.body as string) : []),
    '/admin/openings': OPENINGS,
  });
}

beforeEach(() => {
  localStorage.clear();
  setToken('tok');
  window.location.hash = '#/admin?tab=openings';
});

describe('Админка: вкладка «Начала разговора»', () => {
  it('загружает список, сохраняет его PUT-ом целиком и показывает ошибку загрузки', async () => {
    const fetchMock = renderAdmin();
    const user = userEvent.setup();
    const view = render(
      <AuthProvider>
        <AdminView />
      </AuthProvider>,
    );
    await screen.findByText('admin@example.com');

    // 1) загрузка: строки на месте, счётчик вариантов по грейдам
    expect(await screen.findByDisplayValue('Всем привет')).toBeInTheDocument();
    expect(screen.getByDisplayValue('Привет')).toBeInTheDocument();
    expect(screen.getByText(/Все: 1 · Middle: 1/)).toBeInTheDocument();

    // 2) сохранение: PUT /admin/openings со всем списком (правка — в своём ряду)
    const edited = screen.getByDisplayValue('Привет');
    await user.clear(edited);
    await user.type(edited, 'Здравствуй');
    await user.click(screen.getByRole('button', { name: 'Сохранить все' }));
    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some(
          (c) => c[1]?.method === 'PUT' && String(c[0]).endsWith('/admin/openings'),
        ),
      ).toBe(true),
    );
    const put = fetchMock.mock.calls.find(
      (c) => c[1]?.method === 'PUT' && String(c[0]).endsWith('/admin/openings'),
    ) as [string, RequestInit];
    expect(JSON.parse(put[1].body as string)).toEqual([
      { grade: '*', prompt: 'Всем привет' },
      { grade: 'middle', prompt: 'Здравствуй' },
    ]);

    // 3) ошибка загрузки: 500 → сообщение об ошибке, а не пустой экран
    view.unmount();
    __api.on({ '/admin/openings': __api.failure(500, 'internal', 'начала недоступны') });
    render(
      <AuthProvider>
        <AdminView />
      </AuthProvider>,
    );
    expect(await screen.findByRole('alert')).toHaveTextContent('начала недоступны');
  });
});
