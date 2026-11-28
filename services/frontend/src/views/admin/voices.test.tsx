import { SETTINGS_STUB } from './tabs';
/**
 * Вкладка «Голоса» (src/views/admin/voices.tsx): реестр голосов, сохранение
 * реестра целиком (PUT), ошибка загрузки.
 */
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it } from 'vitest';
import { setToken } from '../../api';
import { AuthProvider } from '../../auth';
import { SettingsView } from '../SettingsView';

const ME_ADMIN = {
  user: { id: 1, email: 'admin@example.com', created_at: '2026-09-14T00:00:00Z', is_admin: true },
};

const REGISTRY = {
  default: 'alice',
  voices: [
    { id: 'v1', name: 'Алиса', gender: 'female', speaker: 'alice' },
    { id: 'v2', name: 'Борис', gender: 'male', speaker: 'boris' },
  ],
};

function renderAdmin() {
  return __api.on({
    '/auth/me': ME_ADMIN,
    'GET /settings': SETTINGS_STUB,
    'PUT /settings': __api.json(200, { applied: {}, applies: 'restart', pending: [] }),
    // сохранение — PUT реестром целиком; точный метод раньше общего GET-ключа
    'PUT /admin/tts-voices': (_url: string, init?: RequestInit) =>
      __api.json(200, init?.body ? JSON.parse(init.body as string) : {}),
    '/admin/tts-voices': REGISTRY,
  });
}

beforeEach(() => {
  localStorage.clear();
  setToken('tok');
  window.location.hash = '#/admin?tab=voices';
});

describe('Админка: вкладка «Голоса»', () => {
  it('загружает реестр, сохраняет его PUT-ом целиком и показывает ошибку загрузки', async () => {
    const fetchMock = renderAdmin();
    const user = userEvent.setup();
    const view = render(
      <AuthProvider>
        <SettingsView />
      </AuthProvider>,
    );
    await screen.findByText('admin@example.com');

    // 1) загрузка: голоса в таблице, default выбран из реестра
    // (getByDisplayValue матчит и select с выбранной опцией — берём внутри таблицы)
    const table = await screen.findByRole('table');
    expect(await within(table).findByDisplayValue('Алиса')).toBeInTheDocument();
    expect(within(table).getByDisplayValue('Борис')).toBeInTheDocument();
    expect(screen.getByLabelText('Голос по умолчанию')).toHaveValue('alice');

    // 2) сохранение: PUT /admin/tts-voices со всем реестром (правка — в своём ряду)
    const edited = within(table).getByDisplayValue('Алиса');
    await user.clear(edited);
    await user.type(edited, 'Алиса В.');
    await user.click(screen.getByRole('button', { name: 'Сохранить все' }));
    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some(
          (c) => c[1]?.method === 'PUT' && String(c[0]).endsWith('/admin/tts-voices'),
        ),
      ).toBe(true),
    );
    const put = fetchMock.mock.calls.find(
      (c) => c[1]?.method === 'PUT' && String(c[0]).endsWith('/admin/tts-voices'),
    ) as [string, RequestInit];
    expect(JSON.parse(put[1].body as string)).toEqual({
      default: 'alice',
      voices: [
        { id: 'v1', name: 'Алиса В.', gender: 'female', speaker: 'alice' },
        { id: 'v2', name: 'Борис', gender: 'male', speaker: 'boris' },
      ],
    });

    // 3) ошибка загрузки: 500 → сообщение об ошибке, а не пустой экран
    view.unmount();
    __api.on({ '/admin/tts-voices': __api.failure(500, 'internal', 'голоса недоступны') });
    render(
      <AuthProvider>
        <SettingsView />
      </AuthProvider>,
    );
    expect(await screen.findByRole('alert')).toHaveTextContent('голоса недоступны');
  });
});
