/**
 * Типы клиента по доменам. Единая точка импорта для остального кода —
 * `import type { Session } from './types'` (или из 'src/api', он реэкспортирует
 * всё это). Файлы-домены: src/types/*.ts.
 */
export * from './types/domain';
export * from './types/session';
export * from './types/livecode';
export * from './types/design';
export * from './types/report';
export * from './types/admin';
