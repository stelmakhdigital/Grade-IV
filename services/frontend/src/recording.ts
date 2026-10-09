/**
 * Режим записи (FR-S8, ADR-009) — чистая логика склейки/отправки
 * (UI и хук используют; тесты без DOM/WS).
 */

/** Минимум речи для отправки (< 1.5 с — «недостаточно речи», не засоряем диалог). */
export const MIN_SPEECH_MS = 1500;

/**
 * Склейка транскрипта записи: финальные сегменты + текущий интерим —
 * один текст ответа кандидата (пробелы нормализованы).
 */
export function mergeRecording(segs: string[], partial: string): string {
  return [...segs, partial]
    .map((s) => s.trim())
    .filter((s) => s !== '')
    .join(' ')
    .replace(/\s+/g, ' ')
    .trim();
}

/** «Недостаточно речи»: пустой транскрипт или речь < MIN_SPEECH_MS. */
export function isInsufficientSpeech(merged: string, totalSpeechMs: number): boolean {
  return merged === '' || totalSpeechMs < MIN_SPEECH_MS;
}

/** Длительность записи «мм:сс» (для окна записи). */
export function formatRecDuration(startedAtMs: number | null, nowMs: number): string {
  if (startedAtMs === null) return '00:00';
  const s = Math.max(0, Math.floor((nowMs - startedAtMs) / 1000));
  const m = Math.floor(s / 60);
  return `${m.toString().padStart(2, '0')}:${(s % 60).toString().padStart(2, '0')}`;
}
