/** Сессия интервью и её события (ARCHITECTURE.md §4). */
import type { Grade, SessionStatus, Stack, Stage } from './domain';

export interface Session {
  id: number;
  grade: Grade;
  /** Грейд по резюме (ИИ): цель grade — на шаг выше. Пусто — старт с явным грейдем. */
  base_grade?: Grade;
  /** Должность из резюме — по ней заголовок интервью, а не по названию роли. */
  resume_title?: string;
  /** Языки, заявленные в резюме; первый — кодовая стадия (stack). */
  resume_stacks?: Stack[];
  stack: Stack;
  track_id?: string;
  stage: Stage;
  status: SessionStatus;
  duration_limit_s: number;
  active_seconds: number;
  time_left_s: number;
  started_at: string;
  finished_at?: string;
  paused_at?: string;
}

export interface SessionEvent {
  seq: number;
  ts: string;
  kind: string;
  data: Record<string, unknown>;
}
