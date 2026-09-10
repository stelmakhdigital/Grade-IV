/** Базовый путь REST API grade-api (через dev-прокси Vite или nginx в prod). */
export const API_BASE = '/api/v1';

/** Проверка доступности grade-api (GET /api/healthz). */
export async function apiHealth(): Promise<boolean> {
  try {
    const res = await fetch('/api/healthz');
    return res.ok;
  } catch {
    return false;
  }
}
