/** Вход и регистрация (WP-7). */
import { request } from './request';
import type { AuthResult, MeResult } from '../types/domain';

export function register(email: string, password: string): Promise<AuthResult> {
  return request<AuthResult>('/auth/register', { body: { email, password } });
}

export function login(email: string, password: string): Promise<AuthResult> {
  return request<AuthResult>('/auth/login', { body: { email, password } });
}

export function me(): Promise<MeResult> {
  return request<MeResult>('/auth/me', { auth: true });
}

/** Одноразовый тикет для WS-upgrade (spec-14/#12: JWT в query утекает в логи). */
export async function fetchWSTicket(): Promise<string> {
  const r = await request<{ ticket: string }>('/ws-ticket');
  return r.ticket;
}
