/** System Design: сохранение и восстановление холста (ADR-004). */
import { request } from './request';
import type { DesignStructure, WhiteboardState } from '../types/design';

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
