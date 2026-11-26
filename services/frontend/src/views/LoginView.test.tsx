import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it } from 'vitest';
import { setToken } from '../api';
import { useAuth, AuthProvider, type AuthContextValue } from '../auth';
import { LoginView, apiErrorMessage } from './LoginView';
import { ApiError } from '../api';

const AUTH_OK = {
  token: 'tok',
  expires_in_s: 86400,
  user: { id: 1, email: 'cand@example.com', created_at: '2026-09-14T00:00:00Z' },
};

/** Обёртка: AuthProvider + маршруты auth; возвращает fetch-mock для ассертов. */
function harness(routes: Record<string, unknown> = {}) {
  const fetchMock = __api.on(routes);
  render(
    <AuthProvider>
      <LoginView />
    </AuthProvider>,
  );
  return fetchMock;
}

beforeEach(() => {
  localStorage.clear();
});

describe('LoginView (WP-7)', () => {
  it('валидация: короткий пароль и не-email блокируют отправку', async () => {
    const fetchMock = harness();
    const user = userEvent.setup();
    await user.type(screen.getByLabelText('Email'), 'не email');
    await user.type(screen.getByLabelText('Пароль'), '123');
    await user.click(screen.getByRole('button', { name: 'Войти' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Некорректный email');
    await user.clear(screen.getByLabelText('Email'));
    await user.type(screen.getByLabelText('Email'), 'cand@example.com');
    await user.click(screen.getByRole('button', { name: 'Войти' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('не короче 8');
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('успешная регистрация: токен сохранён', async () => {
    const fetchMock = harness({ '/auth/register': AUTH_OK });
    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'Регистрация' }));
    await user.type(screen.getByLabelText('Email'), 'cand@example.com');
    await user.type(screen.getByLabelText('Пароль'), 'password1');
    await user.click(screen.getByRole('button', { name: 'Создать аккаунт' }));
    await waitFor(() => expect(localStorage.getItem('grade.token')).toBe('tok'));
    expect(fetchMock.mock.calls[0][0]).toContain('/auth/register');
  });

  it('ошибка email_exists — человекочитаемое сообщение', async () => {
    harness({
      '/auth/register': __api.failure(409, 'email_exists', 'email уже зарегистрирован'),
    });
    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'Регистрация' }));
    await user.type(screen.getByLabelText('Email'), 'cand@example.com');
    await user.type(screen.getByLabelText('Пароль'), 'password1');
    await user.click(screen.getByRole('button', { name: 'Создать аккаунт' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Этот email уже зарегистрирован',
    );
  });

  it('apiErrorMessage: маппинг кодов', () => {
    // spec-14/D8: 401 вне формы входа — «сессия истекла», в форме входа — пароль.
    expect(apiErrorMessage(new ApiError(401, 'unauthorized', 'x'))).toBe(
      'Сессия истекла — войдите заново.',
    );
    expect(apiErrorMessage(new ApiError(401, 'unauthorized', 'x'), { auth: 'login' })).toBe(
      'Неверный email или пароль.',
    );
    expect(apiErrorMessage(new ApiError(0, 'network', 'x'))).toContain('Нет соединения');
    expect(apiErrorMessage(new Error('boom'))).toBe('boom');
    expect(apiErrorMessage('plain')).toBe('Непредвиденная ошибка.');
  });
});
