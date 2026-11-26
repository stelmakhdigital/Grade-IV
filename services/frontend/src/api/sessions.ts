/** Сессии интервью: создание, история, события, пауза/возобновление, профили. */
import { request } from './request';
import type { Grade, InterviewProfile, Stack } from '../types/domain';
import type { Session, SessionEvent } from '../types/session';

/**
 * Создать сессию. Без grade роль и грейд подбирает ИИ по резюме (так делает
 * кабинет); grade указывают явно API-клиенты/смоук.
 */
export function createSession(opts: {
  grade?: Grade;
  stack?: Stack;
  profileId?: number;
  ttsSpeaker?: string;
} = {}): Promise<Session> {
  const body: Record<string, unknown> = {};
  if (opts.grade) body.grade = opts.grade;
  if (opts.stack) body.stack = opts.stack;
  if (opts.profileId) body.profile_id = opts.profileId;
  if (opts.ttsSpeaker) body.tts_speaker = opts.ttsSpeaker;
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

export function listProfiles(): Promise<InterviewProfile[]> {
  return request<InterviewProfile[]>('/profiles', { auth: true });
}
