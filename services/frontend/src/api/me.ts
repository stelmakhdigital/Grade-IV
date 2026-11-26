/** Профиль кандидата: вакансия кампании (F6) и резюме (по нему подбираются роль и грейд). */
import { API_BASE, ApiError, authHeaders, readApiError, request } from './request';
import type { ResumeInfo } from '../types/domain';

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

/** GET /me/resume — состояние резюме кандидата. */
export function getResume(): Promise<ResumeInfo> {
  return request<ResumeInfo>('/me/resume', { auth: true });
}

/**
 * PUT /me/resume (multipart): сохранить описание и/или файл (pdf/docx/doc, ≤ 8 МБ).
 * file не передан — уже приложенный файл остаётся.
 *
 * Multipart идёт мимо request(): тело — не JSON, а Content-Type ставит fetch
 * (там boundary), который транспорт подменять не должен.
 */
export async function saveResume(about: string, file?: File | null): Promise<ResumeInfo> {
  const form = new FormData();
  form.append('about', about);
  if (file) form.append('resume', file);
  let res: Response;
  try {
    res = await fetch(`${API_BASE}/me/resume`, {
      method: 'PUT',
      headers: authHeaders(),
      body: form,
    });
  } catch {
    throw new ApiError(0, 'network', 'нет соединения с сервером');
  }
  if (!res.ok) throw await readApiError(res);
  return (await res.json()) as ResumeInfo;
}

/** DELETE /me/resume — сброс описания и файла. */
export async function deleteResume(): Promise<ResumeInfo> {
  let res: Response;
  try {
    res = await fetch(`${API_BASE}/me/resume`, { method: 'DELETE', headers: authHeaders() });
  } catch {
    throw new ApiError(0, 'network', 'нет соединения с сервером');
  }
  if (!res.ok) throw await readApiError(res);
  return (await res.json()) as ResumeInfo;
}
