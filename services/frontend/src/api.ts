/**
 * Типизированный API-клиент grade-api (ARCHITECTURE.md §4.1, WP-7).
 * Базовый путь через dev-прокси Vite (vite.config.ts) или nginx (prod).
 * Токен хранится в localStorage (ключ STORAGE_TOKEN) и шлётся в Bearer-заголовке.
 */

export const API_BASE = '/api/v1';
export const STORAGE_TOKEN = 'grade.token';

/** Ошибка API с кодом из тела ответа ({code, msg}). */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, msg: string) {
    super(msg);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
  }
}

export interface User {
  id: number;
  email: string;
  created_at: string;
}

export interface AuthResult {
  token: string;
  expires_in_s: number;
  user: User;
  minutes_remaining_s: number;
}

export interface MeResult {
  user: User;
  minutes_remaining_s: number;
}

export type Grade = 'junior' | 'middle' | 'senior' | 'staff';
export type Stack = 'go' | 'python';
export type Stage = 'voice' | 'livecode' | 'design' | 'report';
export type SessionStatus = 'active' | 'paused' | 'finished' | 'aborted';

export interface Session {
  id: number;
  grade: Grade;
  stack: Stack;
  stage: Stage;
  status: SessionStatus;
  duration_limit_s: number;
  active_seconds: number;
  time_left_s: number;
  started_at: string;
  finished_at?: string;
  paused_at?: string;
}

export interface SessionEvent {
  seq: number;
  ts: string;
  kind: string;
  data: Record<string, unknown>;
}

export interface RunTest {
  name: string;
  passed: boolean;
  output?: string;
}

export interface RunResult {
  exit_code: number;
  stdout: string;
  stderr: string;
  duration_ms: number;
  passed: boolean;
  tests: RunTest[];
}

/** Токен для запроса (localStorage). */
export function getToken(): string | null {
  return localStorage.getItem(STORAGE_TOKEN);
}

export function setToken(token: string | null): void {
  if (token === null) {
    localStorage.removeItem(STORAGE_TOKEN);
  } else {
    localStorage.setItem(STORAGE_TOKEN, token);
  }
}

interface RequestOptions {
  method?: string;
  body?: unknown;
  auth?: boolean;
}

async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = {};
  if (opts.body !== undefined) {
    headers['Content-Type'] = 'application/json';
  }
  if (opts.auth !== false) {
    const token = getToken();
    if (token) {
      headers['Authorization'] = `Bearer ${token}`;
    }
  }
  let res: Response;
  try {
    res = await fetch(`${API_BASE}${path}`, {
      method: opts.method ?? (opts.body !== undefined ? 'POST' : 'GET'),
      headers,
      body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
    });
  } catch {
    throw new ApiError(0, 'network', 'нет соединения с сервером');
  }
  if (!res.ok) {
    let code = 'internal';
    let msg = `ошибка ${res.status}`;
    try {
      // Бэкенд шлёт {code, message} (writeError); msg — историческое имя.
      const j = (await res.json()) as { code?: string; msg?: string; message?: string };
      if (j.code) code = j.code;
      if (j.message) msg = j.message;
      else if (j.msg) msg = j.msg;
    } catch {
      // тело не JSON — оставляем общие коды
    }
    throw new ApiError(res.status, code, msg);
  }
  if (res.status === 204) {
    return undefined as T;
  }
  return (await res.json()) as T;
}

// ------------------------------------------------------------------ auth

export function register(email: string, password: string): Promise<AuthResult> {
  return request<AuthResult>('/auth/register', { body: { email, password } });
}

export function login(email: string, password: string): Promise<AuthResult> {
  return request<AuthResult>('/auth/login', { body: { email, password } });
}

export function me(): Promise<MeResult> {
  return request<MeResult>('/auth/me', { auth: true });
}

// ---------------------------------------------------------------- sessions

export function createSession(grade: Grade, stack: Stack): Promise<Session> {
  return request<Session>('/sessions', { body: { grade, stack } });
}

export function listSessions(): Promise<Session[]> {
  return request<Session[]>('/sessions', { auth: true });
}

export function getSession(id: number): Promise<Session> {
  return request<Session>(`/sessions/${id}`, { auth: true });
}

export function listEvents(id: number): Promise<SessionEvent[]> {
  return request<SessionEvent[]>(`/sessions/${id}/events`, { auth: true });
}

/** Live-Code: запуск тестов решения (POST /sessions/{id}/runs, §4.4). */
export function runTests(
  id: number,
  files: Record<string, string>,
  taskId?: string,
): Promise<RunResult> {
  return request<RunResult>(`/sessions/${id}/runs`, {
    body: { files, action: 'test', task_id: taskId },
  });
}

/** Структура схемы System Design для оценки ИИ (ADR-004). */
export interface DesignStructure {
  blocks: string[];
  links: number;
}

/** Заголовки авторизации (Bearer, если токен есть). */
function authHeaders(): Record<string, string> {
  const h: Record<string, string> = {};
  const token = getToken();
  if (token) {
    h['Authorization'] = `Bearer ${token}`;
  }
  return h;
}

/** ApiError из тела ответа ({code, message|msg}). */
async function readApiError(res: Response): Promise<ApiError> {
  let code = 'internal';
  let msg = `ошибка ${res.status}`;
  try {
    const j = (await res.json()) as { code?: string; msg?: string; message?: string };
    if (j.code) code = j.code;
    if (j.message) msg = j.message;
    else if (j.msg) msg = j.msg;
  } catch {
    // тело не JSON — общие коды
  }
  return new ApiError(res.status, code, msg);
}

/** Критерий отчёта (шкала 1–5, вес — доля §12). */
export interface ReportCriterion {
  name: string;
  weight: number;
  score: number;
  comment?: string;
}

/** Итоговый отчёт (WP-11, §12). */
export interface Report {
  overall: number;
  grade_recommendation: string;
  criteria: ReportCriterion[];
  strengths: string[];
  weaknesses: string[];
  recommendations: string[];
}

/**
 * Отчёт сессии (GET /sessions/{id}/report).
 * «generating» — 202 (сессия завершена, отчёт генерируется).
 */
export async function getReport(id: number): Promise<Report | 'generating'> {
  let res: Response;
  try {
    res = await fetch(`${API_BASE}/sessions/${id}/report`, { headers: authHeaders() });
  } catch {
    throw new ApiError(0, 'network', 'нет соединения с сервером');
  }
  if (res.status === 202) {
    return 'generating';
  }
  if (!res.ok) {
    throw await readApiError(res);
  }
  return (await res.json()) as Report;
}

/** Сохранение холста System Design (PUT /sessions/{id}/whiteboard, ADR-004). */
export function saveWhiteboard(
  id: number,
  state: unknown,
  structure: DesignStructure,
  pngB64?: string,
): Promise<{ saved: boolean; structure: DesignStructure }> {
  return request(`/sessions/${id}/whiteboard`, {
    method: 'PUT',
    body: { state, structure, ...(pngB64 ? { png: pngB64 } : {}) },
  });
}

/** Проверка доступности grade-api (GET /healthz, без авторизации). */
export async function apiHealth(): Promise<boolean> {
  try {
    const res = await fetch('/healthz');
    return res.ok;
  } catch {
    return false;
  }
}
