import { render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from './App';
import { setToken } from './api';

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status });
}

/** fetch-мок по маршрутам grade-api. */
function mockApi(opts: { me?: unknown; sessions?: unknown } = {}) {
  return vi.fn(async (url: string) => {
    if (url.includes('/auth/me')) {
      return json(opts.me !== undefined ? 200 : 401, opts.me ?? { code: 'unauthorized', msg: 'нет' });
    }
    if (url.includes('/sessions')) {
      return json(200, opts.sessions ?? []);
    }
    return json(404, {});
  }) as unknown as typeof fetch;
}

const ME = {
  user: { id: 1, email: 'cand@example.com', created_at: '2026-09-14T00:00:00Z' },
  minutes_remaining_s: 3600,
};

beforeEach(() => {
  localStorage.clear();
  window.location.hash = '#/';
});

describe('App: маршрутизация (WP-7)', () => {
  it('без токена — экран входа', async () => {
    vi.stubGlobal('fetch', mockApi());
    render(<App />);
    expect(await screen.findByRole('tab', { name: 'Вход' })).toBeInTheDocument();
    expect(screen.getByText('Регистрация')).toBeInTheDocument();
  });

  it('протухший токен (401 /me) — экран входа, токен удалён', async () => {
    setToken('old');
    vi.stubGlobal('fetch', mockApi());
    render(<App />);
    expect(await screen.findByRole('tab', { name: 'Вход' })).toBeInTheDocument();
    expect(localStorage.getItem('grade.token')).toBeNull();
  });

  it('с валидным токеном — кабинет с email и минутами', async () => {
    setToken('tok');
    vi.stubGlobal(
      'fetch',
      mockApi({ me: ME, sessions: [{ id: 1, grade: 'middle', stack: 'go', stage: 'voice', status: 'finished', duration_limit_s: 3000, active_seconds: 2400, time_left_s: 0, started_at: '2026-09-14T10:00:00Z', finished_at: '2026-09-14T10:40:00Z' }] }),
    );
    render(<App />);
    expect(await screen.findByTestId('user-email')).toBeInTheDocument();
    expect(screen.getByTestId('minutes')).toHaveTextContent('60');
    expect(screen.getByText('История')).toBeInTheDocument();
    // findBy: /sessions может резолвиться позже /auth/me (гонка запросов)
    const link = await screen.findByRole('link', { name: 'Транскрипт' });
    expect(link).toHaveAttribute('href', '#/sessions/1');
  });

  it('#/sessions/1 — страница сессии (транскрипт)', async () => {
    setToken('tok');
    const fetchMock = vi.fn(async (url: string) => {
      if (url.includes('/auth/me')) return json(200, ME);
      if (url.endsWith('/sessions/1')) return json(200, { id: 1, grade: 'middle', stack: 'go', stage: 'report', status: 'finished', duration_limit_s: 3000, active_seconds: 3000, time_left_s: 0, started_at: '2026-09-14T10:00:00Z', finished_at: '2026-09-14T10:50:00Z' });
      if (url.includes('/events')) return json(200, [{ seq: 1, ts: '2026-09-14T10:01:00Z', kind: 'user_utterance', data: { text: 'Привет' } }]);
      return json(404, {});
    });
    vi.stubGlobal('fetch', fetchMock);
    window.location.hash = '#/sessions/1';
    render(<App />);
    expect(await screen.findByText('Кандидат')).toBeInTheDocument();
    expect(screen.getByText('Привет')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'В кабинет' })).toHaveAttribute('href', '#/');
  });
});
