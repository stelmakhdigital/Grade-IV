/**
 * Итоговый отчёт (WP-11, §12) и публичная ссылка на него (spec-16).
 *
 * Nullability сверена с Go: interviewer.ReportData (internal/interviewer/
 * report.go) — срезы criteria/stages/livecode_tasks/progress_vs_previous/
 * study_plan_2weeks нормализуются в finish() в [], то есть приходят массивом
 * всегда; `stages`/`livecode_tasks` остаются опциональными, потому что
 * отчёты, сохранённые до spec-24 / spec-13B, просто не содержат этих ключей.
 *
 * Поля, которые пишет LLM (grade_recommendation, verdict, grade_gap,
 * question_trail, name/comment критерия, title/basis стадии, title задачи),
 * имеют тип unknown: Go-структура объявляет их string, но значение приходит из
 * JSON ответа модели и в UI читается как текст. Сужаются на экране —
 * text()/hasText() в views/report/ReportView.tsx.
 */

/** Критерий отчёта (шкала 1–5, вес — доля §12). */
export interface ReportCriterion {
  /** Подпись критерия — из ответа LLM (веса сервер берёт из своей таблицы). */
  name: unknown;
  weight: number;
  score: number;
  /** Комментарий оценщика (LLM). */
  comment?: unknown;
  /** Итерация B: факты-примеры из ответов кандидата. */
  evidence?: string[];
  /** Итерация B: разрыв до целевого грейда (0 = в уровне). */
  gap_to_grade?: number;
  /** Итерация B: 2–3 пункта планa развития по критерию. */
  study_plan?: string[];
}

/** Сравнение с прошлым интервью (итерация B). */
export interface ReportProgressEntry {
  session_id: number;
  date: string;
  stack: string;
  grade: string;
  overall: number;
  same_stack: boolean;
  criteria_delta: Record<string, number>;
}

/** Пер-task результат Live-Code (spec-13B T3). */
export interface ReportLiveCodeTask {
  id: string;
  /** Название задачи — из банка задач (генерируется ИИ). */
  title: unknown;
  /** passed | failed | no_submission — считается сервером по событиям. */
  status: string;
}

/** Оценка стадии по данным сессии (spec-24): число с основанием, его можно
 * пересчитать по событиям сессии, а не только поверить оценщику. */
export interface ReportStage {
  stage: string;
  /** Подпись стадии (LLM/сервер). */
  title: unknown;
  score: number;
  /** Что посчитано (коротко, человеком). */
  basis: unknown;
  evidence?: string[];
  /** Обязательные артефакты доски, которых не оказалось. */
  missing?: string[];
}

/** Итоговый отчёт (WP-11, §12; итерация B — детальные поля для кандидата). */
export interface Report {
  overall: number;
  /** Вывод по грейду (LLM). */
  grade_recommendation: unknown;
  /** Итерация B: вывод в 1–2 предложения (LLM). */
  verdict?: unknown;
  criteria: ReportCriterion[];
  strengths: string[];
  weaknesses: string[];
  recommendations: string[];
  /** Итерация B: план подготовки на 2 недели. */
  study_plan_2weeks?: string[];
  /** Итерация B: разрыв до целевого грейда (текст, LLM). */
  grade_gap?: unknown;
  /** Путь по уровням вопросов: где держал цель, где сложность снижали (LLM). */
  question_trail?: unknown;
  /** Итерация B: прогресс по прошлым интервью. */
  progress_vs_previous?: ReportProgressEntry[];
  /** spec-13B T3: per-task результаты Live-Code. */
  livecode_tasks?: ReportLiveCodeTask[];
  /** spec-24: оценки по стадиям (детерминированный слой под суждением ИИ). */
  stages?: ReportStage[];
}

/**
 * Отчёт сессии (GET /sessions/{id}/report).
 * «generating» — 202 (сессия завершена, отчёт генерируется).
 */
export type ReportResult = Report | 'generating';

/** Публичная ссылка на отчёт (F1-export, spec-16). */
export interface ShareInfo {
  token: string;
  /** Хеш-путь страницы: /share/{token}. */
  path: string;
}

/** Публичный отчёт = Report + метаданные сессии (без персональных данных). */
export interface PublicReport extends Report {
  meta: { grade: string; stack: string; finished_at?: string };
}
