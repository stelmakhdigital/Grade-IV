/** Отчёт по сессии (WP-11) и публичная ссылка на него (spec-16/F1). */
import { API_BASE, ApiError, authHeaders, readApiError, request } from './request';
import type { PublicReport, Report, ReportResult, ShareInfo } from '../types/report';

/**
 * Отчёт сессии (GET /sessions/{id}/report).
 * «generating» — 202 (сессия завершена, отчёт генерируется).
 *
 * fetch здесь честный, а не через request(): у этого ответа значим САМ статус
 * (202 = «ещё считаем», это не JSON-ошибка), а request() разворачивает тело для
 * любого 2xx. Статус проверяется явно, ошибка — тем же readApiError, сетевой
 * сбой — тем же ApiError(0, 'network').
 */
export async function getReport(id: number): Promise<ReportResult> {
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

/** Создать/переиспользовать публичную ссылку на отчёт сессии (владелец). */
export function shareReport(id: number): Promise<ShareInfo> {
  return request<ShareInfo>(`/sessions/${id}/share`, { method: 'POST' });
}

/** Отозвать публичную ссылку. */
export function revokeShare(id: number): Promise<{ revoked: boolean }> {
  return request<{ revoked: boolean }>(`/sessions/${id}/share`, { method: 'DELETE' });
}

/** Публичный отчёт по токену (без аутентификации). */
export function fetchPublicReport(token: string): Promise<PublicReport> {
  return request<PublicReport>(`/public/reports/${encodeURIComponent(token)}`);
}
