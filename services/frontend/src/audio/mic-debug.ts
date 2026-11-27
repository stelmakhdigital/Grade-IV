/**
 * Dev-диагностика микрофона: флаг live-отчётов и форма снапшота захвата
 * для UI и /debug/mic-report.
 */

// Dev-режим диагностики микрофона: live-отчёты на /debug/mic-report каждые 10 с.
// Включение: VITE_MIC_DEBUG (vite env) — принимаются и '1', и 'true': .env.example
// пишет 1, а vite-документация и тесты — true; молча игнорировать наполовину
// верное значение значит оставить разработчика без диагностики.
// В production-сборке переменная не передаётся — выключено.
// Функция (не константа): значение читается при вызове — тесты стабятся через vi.stubEnv.
export function micDebugEnabled(): boolean {
  const v = import.meta.env.VITE_MIC_DEBUG ?? '';
  return v === '1' || v === 'true';
}

export interface MicDebugInfo {
  path: 'worklet' | 'fallback';
  ctxState: AudioContextState | null;
  rate: number;
  chunks: number;
  medGapMs: number;
  maxRms: number;
}
