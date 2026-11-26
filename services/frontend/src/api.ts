/**
 * Типизированный API-клиент grade-api (ARCHITECTURE.md §4.1, WP-7).
 *
 * Реализация разложена по доменам — src/api/*.ts, весь HTTP идёт через
 * src/api/request.ts. Файл остаётся единой точкой импорта: `import { login,
 * type Session } from '../api'` работает как раньше (здесь реэкспорт всего,
 * что было в едином api.ts, плюс доменные типы из src/types).
 *
 * Порядок star-экспортов конфликтов не даёт: имена в доменах не повторяются,
 * а типы приходят одним источником (./types).
 */
export * from './types';

export { API_BASE, STORAGE_TOKEN, ApiError, getToken, setToken } from './api/request';

export * from './api/auth';
export * from './api/me';
export * from './api/sessions';
export * from './api/livecode';
export * from './api/design';
export * from './api/report';
export * from './api/tracks';
export * from './api/voice';
export * from './api/admin';
