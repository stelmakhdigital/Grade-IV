/**
 * Админка (итерация 11): шаблоны, начала разговора, реестр голосов, роли
 * собеседований (spec-18/22). Все пути — /admin/*.
 */
import { request } from './request';
import type { TTSVoiceRegistry } from '../types/domain';
import type { AdminOpening, AdminTemplate, AdminTrack } from '../types/admin';

// ------------------------------------------------------------ шаблоны

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

// ------------------------------------------------------ начала разговора

export function adminListOpenings(): Promise<AdminOpening[]> {
  return request<AdminOpening[]>('/admin/openings', { auth: true });
}

export function adminPutOpenings(list: AdminOpening[]): Promise<AdminOpening[]> {
  return request<AdminOpening[]>('/admin/openings', { method: 'PUT', body: list, auth: true });
}

// --------------------------------------------------------- реестр голосов

export function adminListTTSVoices(): Promise<TTSVoiceRegistry> {
  return request<TTSVoiceRegistry>('/admin/tts-voices', { auth: true });
}

export function adminPutTTSVoices(reg: TTSVoiceRegistry): Promise<TTSVoiceRegistry> {
  return request<TTSVoiceRegistry>('/admin/tts-voices', { method: 'PUT', body: reg, auth: true });
}

// --------------------------------------------------------- роли (треки)

/** Полный список треков (админ-редактор, spec-18/Ф4). */
export function adminListTracks(): Promise<AdminTrack[]> {
  return request<AdminTrack[]>('/admin/tracks', { auth: true });
}

/**
 * spec-22: черновик роли по описанию (ИИ). НЕ сохраняется — админ правит
 * в форме и сохраняет adminCreateTrack. 502 — ИИ недоступен (fallback —
 * ручное заполнение).
 */
export function adminGenerateTrack(description: string): Promise<AdminTrack> {
  return request<{ track: AdminTrack }>('/admin/tracks/generate', {
    body: { description },
    auth: true,
  }).then((r) => r.track);
}

export function adminCreateTrack(t: AdminTrack): Promise<AdminTrack> {
  return request<AdminTrack>('/admin/tracks', { body: { track: t }, auth: true });
}

export function adminUpdateTrack(t: AdminTrack): Promise<AdminTrack> {
  // method обязателен: без него request идёт POST (method по умолчанию при body),
  // а PUT /admin/tracks/{id} — единственный принявший правку маршрут.
  return request<AdminTrack>(`/admin/tracks/${t.id}`, { method: 'PUT', body: { track: t }, auth: true });
}

export function adminDeleteTrack(id: string): Promise<{ ok: boolean }> {
  return request<{ ok: boolean }>(`/admin/tracks/${id}`, { method: 'DELETE', auth: true });
}

// ------------------------------------------------- система (контур настроек)

/** Значение настройки вместе с тем, откуда оно взялось. */
export type SettingSource = {
  value: string | number | boolean | null;
  source: 'env' | 'file' | 'default';
  env: string;
};

/** Модель из каталога voice: есть ли её веса на диске и чего не хватает. */
export type ModelOption = { name: string; present: boolean; missing: string[] };

/** Блок voice в GET /admin/runtime (прямая проксикация /config/models). */
export type VoiceRuntime = {
  error?: string;
  path?: string;
  writable?: boolean;
  applies?: string;
  applies_note?: string;
  current?: {
    stt?: { provider?: string; model?: string; device?: string; loaded?: boolean };
    tts?: { provider?: string; model?: string; loaded?: boolean; speakers?: string[] };
  };
  sources?: {
    stt?: {
      provider?: SettingSource;
      model?: Record<string, SettingSource>;
    };
    tts?: { provider?: SettingSource; speaker?: SettingSource };
  };
  catalog?: { stt?: Record<string, ModelOption[]>; tts?: Record<string, ModelOption[]> };
};

export type RuntimeView = {
  app_env: string;
  applies: string;
  applies_note: string;
  llm: {
    base_url: SettingSource;
    model: SettingSource;
    enable_thinking: SettingSource;
    mock: boolean;
    api_key_set: boolean;
    config_path: string;
  };
  voice: VoiceRuntime;
  sandbox: { url: string; admin_token_set: boolean; tasks_refill_s: number; tasks_min: number };
};

/** Что меняем: блоки независимы (LLM живёт в файле api, голоса — в voice). */
export type ModelsPut = {
  llm?: { base_url?: string; model?: string; enable_thinking?: boolean };
  stt?: { provider: string; model: string };
  tts?: { provider: string; speaker: string };
};

export type ModelsPutResult = {
  applied: Record<string, { saved?: boolean; applies?: string; status?: number; error?: string; note?: string }>;
  applies: string;
};

export function adminRuntime(): Promise<RuntimeView> {
  return request<RuntimeView>('/admin/runtime', { auth: true });
}

export function adminPutModels(payload: ModelsPut): Promise<ModelsPutResult> {
  return request<ModelsPutResult>('/admin/models', { method: 'PUT', body: payload, auth: true });
}
