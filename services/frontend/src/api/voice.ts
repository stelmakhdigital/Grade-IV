/** Голос интервьюера (F1, Итерация 10): реестр из config/tts-voices.json. */
import { request } from './request';
import type { TTSVoiceRegistry } from '../types/domain';

/** Реестр голосов интервьюера для кандидата (GET /api/v1/tts-voices). */
export function listTTSVoices(): Promise<TTSVoiceRegistry> {
  return request<TTSVoiceRegistry>('/tts-voices', { auth: true });
}
