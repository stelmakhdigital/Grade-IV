/**
 * Общие доменные типы клиента (WP-7) — то, что не относится к одной стадии.
 * Типы описывают ответы grade-api: поля и nullability сверены с Go-структурами
 * (services/api/internal/models, internal/httpapi, internal/interviewer),
 * а не с «как было бы удобно».
 */

export type Grade = 'junior' | 'middle' | 'senior' | 'staff';
export type Stack = 'go' | 'python' | 'shell';
export type Stage = 'voice' | 'livecode' | 'design' | 'report';
export type SessionStatus = 'active' | 'paused' | 'finished' | 'aborted';

export interface User {
  id: number;
  email: string;
  created_at: string;
  is_admin: boolean;
}

export interface AuthResult {
  token: string;
  expires_in_s: number;
  user: User;
}

export interface MeResult {
  user: User;
}

/** Профиль резюме кандидата: своё описание + факт загрузки файла. */
export interface ResumeInfo {
  about: string;
  file_name: string;
  /** Символов текста, извлечённого из файла (pdf/docx). */
  text_chars: number;
}

/** Профиль интервьюера (tone × difficulty, итерация B). Влияет только на стиль вопросов. */
export interface InterviewProfile {
  id: number;
  name: string;
  tone: 'strict' | 'balanced' | 'supportive' | 'playful' | 'socratic';
  difficulty: 'minus' | 'standard' | 'plus';
  is_preset: boolean;
  description?: string;
}

/** Голос интервьюера (F1, Итерация 10): из реестра config/tts-voices.json. */
export interface TTSVoice {
  id: string;
  name: string;
  gender: string;
  speaker: string;
}

export interface TTSVoiceRegistry {
  default: string;
  voices: TTSVoice[];
}
