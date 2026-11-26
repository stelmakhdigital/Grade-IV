/** Админ-конфигурации (итерация 11) и роли собеседований (spec-18/22). */
import type { Grade, Stack } from './domain';

export interface TemplateBlock {
  title: string;
  focus: string;
  question_count: number;
}

/** Шаблон интервью (admin, итерация 11): соответствует db.Template. */
export interface AdminTemplate {
  id: number;
  name: string;
  grade: Grade;
  stack: string;
  duration_s: number;
  is_default: boolean;
  blocks: TemplateBlock[];
}

/** Реплика-начало (admin, итерация 11). grade: junior|middle|senior|staff|*. */
export interface AdminOpening {
  grade: string;
  prompt: string;
}

/** Трек собеседования (spec-18): профиль роли — состав стадий/доска/критерии. */
/** spec-22: роль собеседования. Практика — часть роли (один runtime); null —
 * роль без практической стадии. Стеков «на выбор кандидата» больше нет. */
export interface Track {
  id: string;
  title: string;
  description?: string;
  practice?: { runtime: Stack } | null;
  grades: Grade[];
  board: {
    title: string;
    groups: { title: string; names: string[]; color?: string }[];
  };
  criteria: { name: string; weight: number }[];
}

/** Полный трек для админ-редактора (с промптами и кейсами). */
export interface AdminTrack extends Track {
  board: Track['board'] & {
    review_prompt: string;
    cases?: { grade: Grade; title: string; statement: string; checklist?: string[] }[];
  };
  criteria: Track['criteria'] & { hint?: string }[];
  grade_confirm?: number;
  /** Незавершённые сессии роли (issue #7): практика меняется, только когда их нет. */
  active_sessions?: number;
}
