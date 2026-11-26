/**
 * Низкоуровневый путь всех запросов к grade-api: один transport для доменных
 * модулей src/api/*..ts (ARCHITECTURE.md §4.1, WP-7).
 *
 * Делает ровно то, что делал request() в едином src/api.ts:
 *   · базовый путь через dev-прокси Vite (vite.config.ts) или nginx (prod);
 *   · токен из localStorage (STORAGE_TOKEN) в Bearer-заголовке;
 *   · тело запроса — JSON (кроме multipart: там fetch сам ставит boundary);
 *   · ошибка разбирается в ApiError{status, code, message};
 *   · 204 → undefined (клиенту нечего читать);
 *   · сетевой сбой → ApiError(0, 'network').
 *
 * Запросов на чужие origin здесь нет: всё, что уходит в grade-api, идёт через
 * request(). Единственный честный fetch вне транспорта — getReport(): ему нужен
 * статус 202 (не JSON-ответ), а не только ok/!ok.
 */

export const API_BASE = '/api/v1';
export const STORAGE_TOKEN = 'grade.token';

/** Ошибка API с кодом из тела ответа ({code, message}). */
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

/** Заголовки авторизации (Bearer, если токен есть). */
export function authHeaders(): Record<string, string> {
  const h: Record<string, string> = {};
  const token = getToken();
  if (token) {
    h['Authorization'] = `Bearer ${token}`;
  }
  return h;
}

/** ApiError из тела ответа ({code, message}). */
export async function readApiError(res: Response): Promise<ApiError> {
  let code = 'internal';
  let msg = `ошибка ${res.status}`;
  try {
    // REST пишет writeError → {code, message}; {code, msg} — только WS-события,
    // их разбирает ws.ts, здесь такой ветки больше нет.
    const j = (await res.json()) as { code?: string; message?: string };
    if (j.code) code = j.code;
    if (j.message) msg = j.message;
  } catch {
    // тело не JSON — оставляем общие коды
  }
  return new ApiError(res.status, code, msg);
}

export interface RequestOptions {
  method?: string;
  body?: unknown;
  auth?: boolean;
}

export async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = {};
  if (opts.body !== undefined) {
    headers['Content-Type'] = 'application/json';
  }
  if (opts.auth !== false) {
    Object.assign(headers, authHeaders());
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
    throw await readApiError(res);
  }
  if (res.status === 204) {
    return undefined as T;
  }
  return (await res.json()) as T;
}
