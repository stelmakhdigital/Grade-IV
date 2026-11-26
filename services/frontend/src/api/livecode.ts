/** Live-Code / практика: запуск тестов решения в sandbox (§4.4). */
import { request } from './request';
import type { RunResult } from '../types/livecode';

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
