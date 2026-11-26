import '@testing-library/jest-dom/vitest';
import { beforeEach, vi, type Mock } from 'vitest';

// Часть состояния живёт в адресе (фильтр истории, вкладка админки, номер
// задачи), а jsdom на файл один: без сброса соседний тест стартует с чужим
// ?f= / ?tab= / ?t=.
beforeEach(() => {
  window.location.hash = '#/';
});

// ---------------------------------------------------------------- __api
/**
 * __api — общий мок HTTP-транспорта тестов. Семантика совпадает с боевым
 * request() (src/api/request.ts), чтобы тест проверял контракт, а не свою
 * самоделку:
 *
 *   __api.on(routes)                — fetch-мок grade-api по маршрутам:
 *                                     значение — тело ответа (200 + JSON),
 *                                     Response (__api.json/__api.failure) или
 *                                     функция (url, init) => Response.
 *                                     Возвращает vi.fn — ассерты по URL,
 *                                     методу и телу идут через .mock.calls.
 *   __api.json(status, body)        — ответ с JSON-телом (204 — без тела).
 *   __api.failure(status, code, msg)— ошибка в форме бэкенда {code, message};
 *                                     боевой request разворачивает её в
 *                                     ApiError{status, code, message}.
 *   __api.noContent()               — 204; request() возвращает undefined.
 *
 * Маршрут — «METHOD /путь» или «/путь» (любой метод): путь сравнивается с
 * концом URL, поэтому '/sessions' не матчит '/sessions/9' (метод обязателен,
 * когда у одного пути разные ответы). Совпадает ПЕРВЫЙ объявленный маршрут —
 * частные ('POST /sessions') пишут раньше общих ('/sessions'). Тот же ключ,
 * объявленный позже, перезаписывает предыдущий: так тест перебивает маршрут,
 * заданный в beforeEach. Маршрута нет → 404 {code:'not_found'} как у grade-api.
 *
 * Обвязка ставит fetch лениво (на первом __api.on в тесте) и только в этом
 * тесте. Точечно транспорт перебивают локальным
 * `vi.stubGlobal('fetch', ...)` прямо в тесте — он сильнее обвязки. Так
 * остаются случаи, которые нельзя выразить маршрутом: транспорт упал
 * (mockRejectedValue — «нет соединения»), счётчик/порядок ответов без
 * функции-маршрута. Это единственный способ переопределить fetch в тесте.
 */

type RouteValue = unknown | Response | ((url: string, init?: RequestInit) => Response | Promise<Response>);

interface ApiBridge {
  /** Ответ с JSON-телом (204 — без тела). */
  json(status: number, body: unknown): Response;
  /** Ошибка в форме бэкенда: {code, message}. */
  failure(status: number, code: string, message: string): Response;
  /** 204: request() возвращает undefined. */
  noContent(): Response;
  /** Маршруты текущего теста; mock для ассертов по URL/методу/телу. */
  on(routes: Record<string, RouteValue>): Mock;
}

function jsonResponse(status: number, body: unknown): Response {
  // 204 не может иметь тела (Response на это бросается), а request() на 204
  // возвращает undefined — то же, что делает бэкенд на DELETE-подобных ответах.
  if (status === 204 || body === undefined) return new Response(null, { status });
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

/** '/sessions' → конец URL; '/admin/tracks*' → любой URL с '/admin/tracks'. */
function matchRoute(key: string, url: string, method: string): boolean {
  const parts = key.trim().split(/\s+/);
  const wantMethod = parts.length > 1 ? parts[0].toUpperCase() : '';
  const path = parts.length > 1 ? parts[1] : parts[0];
  if (wantMethod !== '' && wantMethod !== method) return false;
  if (path === '*') return true;
  if (path.endsWith('*')) return url.includes(path.slice(0, -1));
  return url === path || url.endsWith(path);
}

/** Таблица маршрутов текущего теста (сбрасывается в beforeEach). */
const routes = new Map<string, RouteValue>();
let bridge: Mock | null = null;

const __api: ApiBridge = {
  json: jsonResponse,

  failure(status: number, code: string, message: string): Response {
    return jsonResponse(status, { code, message });
  },

  noContent(): Response {
    return jsonResponse(204, undefined);
  },

  on(next: Record<string, RouteValue>) {
    for (const [key, value] of Object.entries(next)) routes.set(key, value);
    if (bridge === null) {
      bridge = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        const method = (init?.method ?? (init?.body !== undefined ? 'POST' : 'GET')).toUpperCase();
        for (const [key, value] of routes) {
          if (!matchRoute(key, url, method)) continue;
          // Response читается один раз — на каждый запрос своя копия.
          if (value instanceof Response) return value.clone();
          if (typeof value === 'function') return (value as (u: string, i?: RequestInit) => Response)(url, init);
          return jsonResponse(200, value);
        }
        return __api.failure(404, 'not_found', `нет маршрута: ${method} ${url}`);
      });
      vi.stubGlobal('fetch', bridge);
    }
    return bridge;
  },
};

globalThis.__api = __api;

beforeEach(() => {
  routes.clear();
  bridge = null;
});

declare global {
  // eslint-disable-next-line no-var
  var __api: ApiBridge;
}
