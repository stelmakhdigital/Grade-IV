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
  is_admin: boolean;
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

// ------------------------------------------------------------------ Vacancy (F6, итерация 12)

/** Вакансия/кампания кандидата (F6, итерация 12): GET /me/vacancy. */
export function getVacancy(): Promise<{ vacancy_text: string }> {
  return request<{ vacancy_text: string }>('/me/vacancy', { auth: true });
}

/** Сохранить/сбросить вакансию кандидата (F6, итерация 12): PUT /me/vacancy.
    Пустая строка = сброс. Лимит 20000 символов (бэкенд валидирует). */
export function saveVacancy(text: string): Promise<{ vacancy_text: string }> {
  return request<{ vacancy_text: string }>('/me/vacancy', {
    method: 'PUT',
    body: { vacancy_text: text },
    auth: true,
  });
}

// ---------------------------------------------------------------- sessions

export interface InterviewTemplate {
  id: number;
  name: string;
  grade: string;
  stack: string;
  duration_s: number;
  blocks: { title: string; focus: string; question_count: number }[];
  is_default: boolean;
}

export function listTemplates(grade: Grade, stack: Stack): Promise<InterviewTemplate[]> {
  return request<{ templates: InterviewTemplate[] }>(
    `/templates?grade=${grade}&stack=${stack}`, { auth: true },
  ).then((d) => d.templates);
}

/** Профиль интервьюера (tone × difficulty, итерация B). Влияет только на стиль вопросов. */
export interface InterviewProfile {
  id: number;
  name: string;
  tone: 'strict' | 'balanced' | 'supportive' | 'playful' | 'socratic';
  difficulty: 'minus' | 'standard' | 'plus';
  is_preset: boolean;
  description?: string;
}

export function listProfiles(): Promise<InterviewProfile[]> {
  return request<InterviewProfile[]>('/profiles', { auth: true });
}

/** Голос интервьюера (F1, Итерация 10): из реестра config/tts-voices.json. */
export interface TTSVoice {
  id: string;
  name: string;
  gender: string;
  speaker: string;
}

export interface TTSVoiceRegistry {
  default: string;
  voices: TTSVoice[];
}

/** F1 (Итерация 10): реестр голосов интервьюера (GET /api/v1/tts-voices). */
export function listTTSVoices(): Promise<TTSVoiceRegistry> {
  return request<TTSVoiceRegistry>('/tts-voices', { auth: true });
}

// ------------------------------------------------------------------ Admin (итерация 11)

export interface TemplateBlock {
  title: string;
  focus: string;
  question_count: number;
}

/** Шаблон интервью (admin, итерация 11): соответствует db.Template. */
export interface AdminTemplate {
  id: number;
  name: string;
  grade: Grade;
  stack: string;
  duration_s: number;
  is_default: boolean;
  blocks: TemplateBlock[];
}

/** Реплика-начало (admin, итерация 11). grade: junior|middle|senior|staff|*. */
export interface AdminOpening {
  grade: string;
  prompt: string;
}

export function adminListTemplates(): Promise<AdminTemplate[]> {
  return request<AdminTemplate[]>('/admin/templates', { auth: true });
}

export function adminCreateTemplate(t: Omit<AdminTemplate, 'id'>): Promise<AdminTemplate> {
  return request<AdminTemplate>('/admin/templates', { method: 'POST', body: t, auth: true });
}

export function adminUpdateTemplate(id: number, t: Omit<AdminTemplate, 'id'>): Promise<AdminTemplate> {
  return request<AdminTemplate>(`/admin/templates/${id}`, { method: 'PUT', body: t, auth: true });
}

export function adminDeleteTemplate(id: number): Promise<void> {
  return request<void>(`/admin/templates/${id}`, { method: 'DELETE', auth: true });
}

export function adminListOpenings(): Promise<AdminOpening[]> {
  return request<AdminOpening[]>('/admin/openings', { auth: true });
}

export function adminPutOpenings(list: AdminOpening[]): Promise<AdminOpening[]> {
  return request<AdminOpening[]>('/admin/openings', { method: 'PUT', body: list, auth: true });
}

export function adminListTTSVoices(): Promise<TTSVoiceRegistry> {
  return request<TTSVoiceRegistry>('/admin/tts-voices', { auth: true });
}

export function adminPutTTSVoices(reg: TTSVoiceRegistry): Promise<TTSVoiceRegistry> {
  return request<TTSVoiceRegistry>('/admin/tts-voices', { method: 'PUT', body: reg, auth: true });
}

export function createSession(
  grade: Grade,
  stack: Stack,
  templateId?: number,
  profileId?: number,
  ttsSpeaker?: string,
): Promise<Session> {
  const body: Record<string, unknown> = { grade, stack };
  if (templateId) body.template_id = templateId;
  if (profileId) body.profile_id = profileId;
  if (ttsSpeaker) body.tts_speaker = ttsSpeaker;
  return request<Session>('/sessions', { body });
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

/** Пауза сессии (POST /sessions/{id}/pause, FR-S7): тарификация останавливается. */
export function pauseSession(id: number): Promise<Session> {
  return request<Session>(`/sessions/${id}/pause`, { method: 'POST', auth: true });
}

/** Возобновление сессии с паузы (POST /sessions/{id}/resume, FR-S7). */
export function resumeSession(id: number): Promise<Session> {
  return request<Session>(`/sessions/${id}/resume`, { method: 'POST', auth: true });
}

/** Live-Code: запуск тестов решения (POST /sessions/{id}/runs, §4.4). */
export function runTests(
  id: number,
  files: Record<string, string>,
  taskId?: string,
): Promise<RunResult> {
  return request<RunResult>(`/sessions/${id}/runs`, {
    body: { files, action: 'test', task_id: taskId },
  }).then((r) => {
    // BUG-20261011-013: backend отдаёт "tests": null (nil-slice в Go), когда
    // тесты не были запущены (compile-ошибка, нет тулчейна, timeout-setup).
    // Без нормализации панель крашится (result.tests.filter) и размонтирует сессию.
    return r ? { ...r, tests: r.tests ?? [] } : r;
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
  /** Итерация B: факты-примеры из ответов кандидата. */
  evidence?: string[];
  /** Итерация B: разрыв до целевого грейда (0 = в уровне). */
  gap_to_grade?: number;
  /** Итерация B: 2–3 пункта планa развития по критерию. */
  study_plan?: string[];
}

/** Сравнение с прошлым интервью (итерация B). */
export interface ReportProgressEntry {
  session_id: number;
  date: string;
  stack: string;
  grade: string;
  overall: number;
  same_stack: boolean;
  criteria_delta: Record<string, number>;
}

/** Пер-task результат Live-Code (spec-13B T3). */
export interface ReportLiveCodeTask {
  id: string;
  title: string;
  /** passed | failed | no_submission. */
  status: string;
}

/** Итоговый отчёт (WP-11, §12; итерация B — детальные поля для кандидата). */
export interface Report {
  overall: number;
  grade_recommendation: string;
  /** Итерация B: вывод в 1–2 предложения. */
  verdict?: string;
  criteria: ReportCriterion[];
  strengths: string[];
  weaknesses: string[];
  recommendations: string[];
  /** Итерация B: план подготовки на 2 недели. */
  study_plan_2weeks?: string[];
  /** Итерация B: разрыв до целевого грейда (текст). */
  grade_gap?: string;
  /** Итерация B: прогресс по прошлым интервью. */
  progress_vs_previous?: ReportProgressEntry[];
  /** spec-13B T3: per-task результаты Live-Code. */
  livecode_tasks?: ReportLiveCodeTask[];
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

/** Состояние холста Excalidraw (elements + appState) для PUT/GET whiteboard. */
export interface WhiteboardState {
  elements: unknown[];
  appState: unknown;
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

/** Чтение сохранённого холста System Design (GET /sessions/{id}/whiteboard, ADR-004):
    восстановление схемы при возврате на стадию. 404 — схема не сохранялась. */
export function getWhiteboard(
  id: number,
): Promise<{ state: WhiteboardState; structure: DesignStructure }> {
  return request(`/sessions/${id}/whiteboard`);
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

/** Публичная ссылка на отчёт (F1-export, spec-16). */
export interface ShareInfo {
  token: string;
  /** Хеш-путь страницы: /share/{token}. */
  path: string;
}

/** Публичный отчёт = Report + метаданные сессии (без персональных данных). */
export interface PublicReport extends Report {
  meta: { grade: string; stack: string; finished_at?: string };
}

/** Создать/переиспользовать публичную ссылку на отчёт сессии (владелец). */
export async function shareReport(id: number): Promise<ShareInfo> {
  return request<ShareInfo>(`/sessions/${id}/share`, { method: 'POST' });
}

/** Отозвать публичную ссылку. */
export async function revokeShare(id: number): Promise<{ revoked: boolean }> {
  return request<{ revoked: boolean }>(`/sessions/${id}/share`, { method: 'DELETE' });
}

/** Публичный отчёт по токену (без аутентификации). */
export async function fetchPublicReport(token: string): Promise<PublicReport> {
  return request<PublicReport>(`/public/reports/${encodeURIComponent(token)}`);
}
