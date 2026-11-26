import { describe, expect, it, vi, beforeEach } from 'vitest';
import {
  ApiError,
  createSession,
  deleteResume,
  getResume,
  listEvents,
  listSessions,
  login,
  me,
  pauseSession,
  register,
  resumeSession,
  runTests,
  saveResume,
  setToken,
  getToken,
} from './api';

beforeEach(() => {
  localStorage.clear();
});

describe('api-клиент (WP-7)', () => {
  it('register шлёт Bearer-заголовки только при наличии токена и парсит ответ', async () => {
    const fetchMock = __api.on({
      '/auth/register': {
        token: 'tok',
        expires_in_s: 86400,
        user: { id: 1, email: 'a@b.c', created_at: '2026-09-14T00:00:00Z' },
      },
    });
    const r = await register('a@b.c', 'password1');
    expect(r.user.email).toBe('a@b.c');
    expect(r.token).toBe('tok');
    const [url, opts] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/api/v1/auth/register');
    expect((opts.headers as Record<string, string>)['Authorization']).toBeUndefined();
    expect(JSON.parse(opts.body as string)).toEqual({ email: 'a@b.c', password: 'password1' });
  });

  it('me() добавляет Authorization: Bearer из localStorage', async () => {
    setToken('tok123');
    const fetchMock = __api.on({ '/auth/me': { user: { id: 1, email: 'a@b.c', created_at: '' } } });
    await me();
    const [, opts] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect((opts.headers as Record<string, string>)['Authorization']).toBe('Bearer tok123');
  });

  it('401/код из тела -> ApiError со статусом и кодом', async () => {
    __api.on({ '/auth/login': __api.failure(401, 'unauthorized', 'неверный email или пароль') });
    const err = await login('a@b.c', 'wrong').catch((e) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(401);
    expect(err.code).toBe('unauthorized');
    expect(err.message).toBe('неверный email или пароль');
  });

  // Транспорт «упал» маршрутом не выразить (ответа нет вовсе) — единственная
  // заглушка поверх обвязки: локальный vi.stubGlobal('fetch', ...).
  it('сеть недоступна -> ApiError network', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('fail')));
    const err = await listSessions().catch((e) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err.code).toBe('network');
  });

  it('createSession/listSessions/listEvents — пути и методы', async () => {
    const fetchMock = __api.on({
      'POST /sessions': { id: 7, status: 'active' },
      'GET /sessions': [{ id: 7 }],
      '/sessions/7/events': [{ seq: 1, kind: 'finished' }],
    });
    await createSession({ grade: 'middle', stack: 'go' });
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/sessions');
    expect((fetchMock.mock.calls[0][1] as RequestInit).method).toBe('POST');
    expect(JSON.parse((fetchMock.mock.calls[0][1] as RequestInit).body as string)).toEqual({
      grade: 'middle',
      stack: 'go',
    });

    // Пустой вызов — роль и грейд подбирает ИИ по резюме (так делает кабинет).
    await createSession();
    expect(JSON.parse((fetchMock.mock.calls[1][1] as RequestInit).body as string)).toEqual({});

    await listSessions();
    expect(fetchMock.mock.calls[2][0]).toBe('/api/v1/sessions');
    expect((fetchMock.mock.calls[2][1] as RequestInit).method).toBe('GET');

    await listEvents(7);
    expect(fetchMock.mock.calls[3][0]).toBe('/api/v1/sessions/7/events');
  });

  it('резюме: GET/DELETE + PUT multipart с Bearer', async () => {
    setToken('tok123');
    const fetchMock = __api.on({ '/me/resume': { about: '', file_name: '', text_chars: 0 } });

    await getResume();
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/me/resume');

    await saveResume('Go 5 лет', new File(['x'], 'r.pdf', { type: 'application/pdf' }));
    const [, put] = fetchMock.mock.calls[1] as [string, RequestInit];
    expect(put.method).toBe('PUT');
    expect(put.body).toBeInstanceOf(FormData);
    expect((put.body as FormData).get('about')).toBe('Go 5 лет');
    expect(((put.body as FormData).get('resume') as File).name).toBe('r.pdf');
    expect((put.headers as Record<string, string>)['Authorization']).toBe('Bearer tok123');
    // Content-Type не ставим вручную: boundary добавляет fetch.
    expect((put.headers as Record<string, string>)['Content-Type']).toBeUndefined();

    await deleteResume();
    expect((fetchMock.mock.calls[2][1] as RequestInit).method).toBe('DELETE');
  });

  it('pauseSession/resumeSession: POST /sessions/{id}/pause|resume, DTO и Bearer', async () => {
    setToken('tok123');
    const fetchMock = __api.on({
      'POST /sessions/7/pause': { id: 7, status: 'paused', paused_at: '2026-09-14T10:05:00Z' },
      'POST /sessions/7/resume': { id: 7, status: 'active' },
    });
    const s = await pauseSession(7);
    expect(s.status).toBe('paused');
    const [pauseUrl, pauseOpts] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(pauseUrl).toBe('/api/v1/sessions/7/pause');
    expect(pauseOpts.method).toBe('POST');
    expect((pauseOpts.headers as Record<string, string>)['Authorization']).toBe('Bearer tok123');

    const r = await resumeSession(7);
    expect(r.status).toBe('active');
    const [resumeUrl, resumeOpts] = fetchMock.mock.calls[1] as [string, RequestInit];
    expect(resumeUrl).toBe('/api/v1/sessions/7/resume');
    expect(resumeOpts.method).toBe('POST');
  });

  it('pauseSession: код из тела -> ApiError (статус + code + msg)', async () => {
    __api.on({ 'POST /sessions/7/pause': __api.failure(409, 'invalid_state', 'сессия не активна') });
    const err = await pauseSession(7).catch((e) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(409);
    expect(err.code).toBe('invalid_state');
    expect(err.message).toBe('сессия не активна');
  });

  it('runTests: POST /sessions/{id}/runs {files, action, task_id}', async () => {
    const fetchMock = __api.on({
      'POST /sessions/9/runs': {
        exit_code: 0,
        stdout: 'ok',
        stderr: '',
        duration_ms: 12,
        passed: true,
        tests: [{ name: 'TestMain', passed: true }],
      },
    });
    const r = await runTests(9, { 'main.go': 'package main' }, 'go-rev');
    expect(r.passed).toBe(true);
    const [url, opts] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/api/v1/sessions/9/runs');
    expect(JSON.parse(opts.body as string)).toEqual({
      files: { 'main.go': 'package main' },
      action: 'test',
      task_id: 'go-rev',
    });
  });

  it('setToken(null) убирает токен', () => {
    setToken('x');
    expect(getToken()).toBe('x');
    setToken(null);
    expect(getToken()).toBeNull();
  });
});
