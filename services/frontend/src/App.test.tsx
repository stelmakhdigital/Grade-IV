import { render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from './App';
import { setToken } from './api';

/** Маршруты grade-api для роутинга приложения (App.tsx). */
function appRoutes(opts: { me?: unknown; sessions?: unknown } = {}) {
  return {
    // /auth/me без me → 401: протухший токен разлогинивает (auth.tsx).
    '/auth/me': opts.me ?? __api.failure(401, 'unauthorized', 'нет'),
    // Список истории — ровно /sessions. earlier матчился по includes и
    // захватывал /sessions/42 и /sessions/42/report: страница сессии получала
    // массив вместо сессии и падала в ReportBody на criteria.map (красный CI
    // 2026-11-26, при этом локально тест успевал закончиться до асинхронного
    // рендера). Ключ без '*' сравнивается с концом URL — /sessions/42 мимо.
    '/sessions': opts.sessions ?? [],
  };
}

const ME = {
  user: { id: 1, email: 'cand@example.com', created_at: '2026-09-14T00:00:00Z' },
};

beforeEach(() => {
  localStorage.clear();
  window.location.hash = '#/';
});

describe('App: маршрутизация (WP-7)', () => {
  it('без токена — экран входа', async () => {
    __api.on(appRoutes());
    render(<App />);
    expect(await screen.findByRole('button', { name: 'Вход' })).toBeInTheDocument();
    expect(screen.getByText('Регистрация')).toBeInTheDocument();
  });

  it('протухший токен (401 /me) — экран входа, токен удалён', async () => {
    setToken('old');
    __api.on(appRoutes());
    render(<App />);
    expect(await screen.findByRole('button', { name: 'Вход' })).toBeInTheDocument();
    expect(localStorage.getItem('grade.token')).toBeNull();
  });

  it('с валидным токеном — кабинет с email и длительностью интервью', async () => {
    setToken('tok');
    __api.on(appRoutes({ me: ME, sessions: [{ id: 1, grade: 'middle', stack: 'go', stage: 'voice', status: 'finished', duration_limit_s: 3000, active_seconds: 2400, time_left_s: 0, started_at: '2026-09-14T10:00:00Z', finished_at: '2026-09-14T10:40:00Z' }]}));
    render(<App />);
    expect(await screen.findByTestId('user-email')).toBeInTheDocument();
    // Баланса-валюты нет; время интервью — метрика (2400 с → «40 мин»).
    expect(await screen.findByTestId('total-time')).toHaveTextContent('40 мин');
    expect(screen.getByTestId('total-time')).toHaveTextContent('40 мин');
    expect(screen.getByText('История')).toBeInTheDocument();
    // findBy: /sessions может резолвиться позже /auth/me (гонка запросов)
    const link = await screen.findByRole('link', { name: 'Отчёт' });
    expect(link).toHaveAttribute('href', '#/sessions/1');
  });

  it('#/sessions/1 — страница сессии (транскрипт)', async () => {
    setToken('tok');
    __api.on({
      '/auth/me': ME,
      '/sessions/1': { id: 1, grade: 'middle', stack: 'go', stage: 'report', status: 'finished', duration_limit_s: 3000, active_seconds: 3000, time_left_s: 0, started_at: '2026-09-14T10:00:00Z', finished_at: '2026-09-14T10:50:00Z' },
      '/events': [{ seq: 1, ts: '2026-09-14T10:01:00Z', kind: 'user_utterance', data: { text: 'Привет' } }],
    });
    window.location.hash = '#/sessions/1';
    render(<App />);
    expect(await screen.findByText('Кандидат')).toBeInTheDocument();
    expect(screen.getByText('Привет')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'В кабинет' })).toHaveAttribute('href', '#/');
  });

  it('не-админ на #/admin — состояние «нет прав», а не обвес админки', async () => {
    window.location.hash = '#/admin';
    setToken('tok');
    __api.on(appRoutes({ me: ME }));
    render(<App />);
    expect(await screen.findByText(/Недостаточно прав/)).toBeInTheDocument();
    expect(screen.queryByRole('tab', { name: 'Шаблоны' })).not.toBeInTheDocument();
  });

  it('заголовок вкладки различает экраны', async () => {
    setToken('tok');
    __api.on(appRoutes({ me: ME }));
    const view = render(<App />);
    await screen.findByTestId('user-email');
    expect(document.title).toBe('Грейд');
    window.location.hash = '#/sessions/42';
    await vi.waitFor(() => expect(document.title).toBe('Интервью #42 — Грейд'));
    // снять монтирование: незакрытый запрос долетает уже после теста
    view.unmount();
    window.location.hash = '#/';
  });

});
