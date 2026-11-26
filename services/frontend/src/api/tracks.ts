/** Роли собеседований (spec-18/22): список для кабинета. */
import { request } from './request';
import type { Track } from '../types/admin';

/** Список треков (кабинет: выбор роли перед интервью). */
export function listTracks(): Promise<Track[]> {
  return request<Track[]>('/tracks', { auth: true });
}
