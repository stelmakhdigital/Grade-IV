import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { setToken } from '../api';
import { useAuth, AuthProvider, type AuthContextValue } from '../auth';
import { LoginView, apiErrorMessage } from './LoginView';
import { ApiError } from '../api';

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status });
}

const AUTH_OK = {
  token: 'tok',
  expires_in_s: 86400,
  user: { id: 1, email: 'cand@example.com', created_at: '2026-09-14T00:00:00Z' },
  minutes_remaining_s: 3600,
};

/** Обёртка: AuthProvider с подменённым useAuth для изоляции полей. */
function harness(fetchMock: ReturnType<typeof vi.fn>) {
  vi.stubGlobal('fetch', fetchMock);
  return render(
    <AuthProvider>
      <LoginView />
    </AuthProvider>,
  );
}

beforeEach(() => {
  localStorage.clear();
});

describe('LoginView (WP-7)', () => {
  it('валидация: короткий пароль и не-email блокируют отправку', async () => {
    const fetchMock = vi.fn();
    harness(fetchMock);
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
    const fetchMock = vi.fn().mockResolvedValue(json(201, AUTH_OK));
    harness(fetchMock);
    const user = userEvent.setup();
    await user.click(screen.getByRole('tab', { name: 'Регистрация' }));
    await user.type(screen.getByLabelText('Email'), 'cand@example.com');
    await user.type(screen.getByLabelText('Пароль'), 'password1');
    await user.click(screen.getByRole('button', { name: 'Создать аккаунт' }));
    await waitFor(() => expect(localStorage.getItem('grade.token')).toBe('tok'));
    expect(fetchMock.mock.calls[0][0]).toContain('/auth/register');
  });

  it('ошибка email_exists — человекочитаемое сообщение', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(json(409, { code: 'email_exists', msg: 'email уже зарегистрирован' }));
    harness(fetchMock);
    const user = userEvent.setup();
    await user.click(screen.getByRole('tab', { name: 'Регистрация' }));
    await user.type(screen.getByLabelText('Email'), 'cand@example.com');
    await user.type(screen.getByLabelText('Пароль'), 'password1');
    await user.click(screen.getByRole('button', { name: 'Создать аккаунт' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Этот email уже зарегистрирован',
    );
  });

  it('apiErrorMessage: маппинг кодов', () => {
    expect(apiErrorMessage(new ApiError(401, 'unauthorized', 'x'))).toBe(
      'Неверный email или пароль.',
    );
    expect(apiErrorMessage(new ApiError(0, 'network', 'x'))).toContain('Нет соединения');
    expect(apiErrorMessage(new Error('boom'))).toBe('boom');
    expect(apiErrorMessage('plain')).toBe('Непредвиденная ошибка.');
  });
});
