/// <reference types="vitest/config" />
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { fileURLToPath } from 'node:url';

// BUG-20261011-015: es6-promise-pool@2.5.0 (транзитивная зависимость
// @excalidraw/excalidraw) — UMD-файл, ломающийся, когда в окне уже есть
// глобальный AMD define (к дизайн-стадии его инжектят vendor-бандлы):
// UMD уходит в AMD-ветку, default-экспорт становится {} → «not a constructor»
// → падает loadFontFaces и PNG-экспорт схемы. Aliас на чистую ESM-копию
// (src/vendor, тело без изменений) — в dev и prod.
const es6PromisePoolShim = fileURLToPath(new URL('./src/vendor/es6-promise-pool.mjs', import.meta.url));

// Dev-прокси: /api, /healthz, /debug и /ws -> grade-api. В prod — nginx (nginx.conf).
//
// GRADE_API_URL — адрес api, когда он не :8000. На этой машине :8000 занят
// другим контейнером (vLLM: несуществующий путь отвечает 404 {"detail":"Not
// Found"}), и вход/регистрация выглядели как «404 от продукта», хотя запрос до
// api не доходил вовсе. Запуск с другим портом: GRADE_API_URL=http://localhost:8010.
const API = process.env.GRADE_API_URL ?? 'http://localhost:8000';
const WS = API.replace(/^http/, 'ws');
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      'es6-promise-pool': es6PromisePoolShim,
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': { target: API },
      '/healthz': { target: API },
      '/ws': { target: WS, ws: true },
      // spec-14/D11: dev-диагностика микрофона — same-origin через прокси
      // (раньше debug-reporter ломался за прокси/HTTPS).
      '/debug': { target: API },
    },
  },
  build: {
    // B2: лимит предупреждения о размере чанка. Ядро Excalidraw неразбивно
    // (~1.8 MB raw / 737 kB gzip) — это ленивый чанк, грузится только на
    // дизайн-стадии. Попробовано: склейка vendor в один чанк = 1.4 MB gzip
    // монолит (хуже), гранулярный разрез Vite по диаграммным модулям Mermaid
    // — лучший результат. Поэтому порог поднят до реального максимума, а
    // first-load отдельно уменьшен ленивой загрузкой панелей (SessionView).
    chunkSizeWarningLimit: 1900,
    rollupOptions: {
      output: {
        // B2: react-рантайм — в стабильный отдельный чанк (кэшируется между
        // версиями приложения, entry остаётся лёгким). Остальные vendor
        // (Excalidraw/Mermaid/Cytoscape/KaTeX) не сливаем вручную: Vite сам
        // делает гранулярный ленивый split, а склейка в один чанк ухудшает.
        manualChunks(id) {
          if (!id.includes('node_modules')) return undefined;
          // B2: react-рантайм — в стабильный отдельный чанк (кэшируется между
          // версиями, entry остаётся лёгким). Матч по РЕАЛЬНЫМ путям пакетов
          // (не по .pnpm-каталогам с peer-deps, где встречается "react-dom@").
          if (
            /node_modules[\\/]react-dom[\\/]/.test(id) ||
            /node_modules[\\/]react[\\/]/.test(id) ||
            /node_modules[\\/]scheduler[\\/]/.test(id)
          ) {
            return 'vendor-react';
          }
          return undefined;
        },
      },
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['src/test/setup.ts'],
  },
});
