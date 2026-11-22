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

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

beforeEach(() => {
  localStorage.clear();
});

describe('api-клиент (WP-7)', () => {
  it('register шлёт Bearer-заголовки только при наличии токена и парсит ответ', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(201, {
        token: 'tok',
        expires_in_s: 86400,
        user: { id: 1, email: 'a@b.c', created_at: '2026-09-14T00:00:00Z' },
      }),
    );
    vi.stubGlobal('fetch', fetchMock);
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
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, { user: { id: 1, email: 'a@b.c', created_at: '' } }),
    );
    vi.stubGlobal('fetch', fetchMock);
    await me();
    const [, opts] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect((opts.headers as Record<string, string>)['Authorization']).toBe('Bearer tok123');
  });

  it('401/код из тела -> ApiError со статусом и кодом', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(jsonResponse(401, { code: 'unauthorized', msg: 'неверный email или пароль' })),
    );
    const err = await login('a@b.c', 'wrong').catch((e) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(401);
    expect(err.code).toBe('unauthorized');
    expect(err.message).toBe('неверный email или пароль');
  });

  it('сеть недоступна -> ApiError network', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('fail')));
    const err = await listSessions().catch((e) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err.code).toBe('network');
  });

  it('createSession/listSessions/listEvents — пути и методы', async () => {
    const fetchMock = vi.fn(async (..._args: [string, RequestInit]) =>
      jsonResponse(201, { id: 7, status: 'active' }));
    vi.stubGlobal('fetch', fetchMock);
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

    fetchMock.mockResolvedValue(jsonResponse(200, [{ id: 7 }]));
    await listSessions();
    expect(fetchMock.mock.calls[2][0]).toBe('/api/v1/sessions');
    expect((fetchMock.mock.calls[2][1] as RequestInit).method).toBe('GET');

    fetchMock.mockResolvedValue(jsonResponse(200, [{ seq: 1, kind: 'finished' }]));
    await listEvents(7);
    expect(fetchMock.mock.calls[3][0]).toBe('/api/v1/sessions/7/events');
  });

  it('резюме: GET/DELETE + PUT multipart с Bearer', async () => {
    setToken('tok123');
    const fetchMock = vi.fn(async (..._args: [string, RequestInit]) =>
      jsonResponse(200, { about: '', file_name: '', text_chars: 0 }));
    vi.stubGlobal('fetch', fetchMock);

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
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, { id: 7, status: 'paused', paused_at: '2026-09-14T10:05:00Z' }),
    );
    vi.stubGlobal('fetch', fetchMock);
    const s = await pauseSession(7);
    expect(s.status).toBe('paused');
    const [pauseUrl, pauseOpts] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(pauseUrl).toBe('/api/v1/sessions/7/pause');
    expect(pauseOpts.method).toBe('POST');
    expect((pauseOpts.headers as Record<string, string>)['Authorization']).toBe('Bearer tok123');

    fetchMock.mockResolvedValue(jsonResponse(200, { id: 7, status: 'active' }));
    const r = await resumeSession(7);
    expect(r.status).toBe('active');
    const [resumeUrl, resumeOpts] = fetchMock.mock.calls[1] as [string, RequestInit];
    expect(resumeUrl).toBe('/api/v1/sessions/7/resume');
    expect(resumeOpts.method).toBe('POST');
  });

  it('pauseSession: код из тела -> ApiError (статус + code + msg)', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(jsonResponse(409, { code: 'invalid_state', msg: 'сессия не активна' })),
    );
    const err = await pauseSession(7).catch((e) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(409);
    expect(err.code).toBe('invalid_state');
    expect(err.message).toBe('сессия не активна');

    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('fail')));
    const netErr = await resumeSession(7).catch((e) => e);
    expect(netErr).toBeInstanceOf(ApiError);
    expect(netErr.code).toBe('network');
  });

  it('runTests: POST /sessions/{id}/runs {files, action, task_id}', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(200, { exit_code: 0, stdout: 'ok', stderr: '', duration_ms: 12, passed: true, tests: [{ name: 'TestMain', passed: true }] }),
    );
    vi.stubGlobal('fetch', fetchMock);
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
