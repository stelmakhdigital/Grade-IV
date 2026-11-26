# BUG-20261011-015 · System Design: PNG-экспорт схемы падает — оценка ИИ без картинки

- **Статус:** исправлено
- **Область:** `services/frontend` (vite.config.ts + `src/vendor/es6-promise-pool.mjs`),
  транзитивная зависимость `es6-promise-pool@2.5.0` (UMD) пакета `@excalidraw/excalidraw`

## Симптом
На дизайн-стадии «Оценить схему» сохранял схему и структуру, но **PNG-картинку не
отправлял** (в `whiteboards.png_path` — пусто). ИИ-оценка шла по структуре без
картинки (vision-часть ADR-004 фактически отключена). В консоли/ошибках страницы:

```
TypeError: import_es6_promise_pool.default is not a constructor
    at new PromisePool (…/.vite/deps/chunk-*.js)
    at _Fonts2.loadFontFaces (…/.vite/deps/chunk-*.js)
    at _Fonts2.loadSceneFonts (…/.vite/deps/chunk-*.js)
```

## Корень
`es6-promise-pool@2.5.0` — UMD-файл:
`if (typeof define === 'function' && define.amd) { define([], factory) } else if
(typeof exports === 'object') { module.exports = factory() } …`.
К моменту дизайн-стадии в окне **уже определён глобальный AMD-define с `.amd=true`**
(инжектят vendor-бандлы; проверено: на логин-странице `window.define === undefined`,
на дизайн-стадии — функция с `.amd`). UMD уходит в AMD-ветку, `module.exports`
остаётся пустым объектом → `default`-экспорт в бандле = `{}` → `new …default()`
падает. Ломается `loadFontFaces` Excalidraw → `loadSceneFonts` → `exportToBlob`
(наш `exportCanvasPng` ловит ошибку и молча отдаёт `pngB64: null`).
В dev это Vite-пребандл, в prod-бандле Rollup — тот же UMD-код, та же латентная уязвимость.

## Фикс
- `src/vendor/es6-promise-pool.mjs` — ESM-копия модуля (MIT, тело factory без
  изменений, UMD-детекция убрана). Работает независимо от глобальных define/exports.
- `vite.config.ts`: `resolve.alias['es6-promise-pool']` → на вендор-копию
  (деп-специфайер в dist @excalidraw/excalidraw перехватывается и в dev, и в prod).

## Проверка
- Node: `new (await import('…/es6-promise-pool.mjs')).default(['x','y'], 2).start()` —
  пул отрабатывает, `PromisePoolEvent` экспортируется.
- Live (Playwright, дизайн-стадия): ошибок es6-promise-pool нет; после «Оценить схему»
  в `whiteboards` (сессия 69): `png_path` = 20 273 байта с сигнатурой `\x89PNG`,
  elements=6, structure={blocks:3, links:0}.
