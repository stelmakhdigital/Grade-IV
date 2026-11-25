#!/usr/bin/env node
/**
 * Палитра приложения (services/frontend/src/tokens.css) и лендинга (styles.css)
 * живёт в двух файлах: лендинг собирается без vite и не может сделать @import.
 * Проверка не даёт значениям разойтись — ровно из-за чего лендинг и кабинет
 * выглядели разными продуктами (замерено в браузере, ui-review 2026-11-24).
 */
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const app = readFileSync(join(root, 'services/frontend/src/tokens.css'), 'utf8').toLowerCase();
const landing = readFileSync(join(root, 'styles.css'), 'utf8').toLowerCase();

const shared = {
  '#080b11': 'фон',
  '#0d111a': 'панель',
  '#131926': 'карточка',
  '#232c3b': 'линия',
  '#e3e8f2': 'текст',
  '#8f9bb0': 'подписи (AA на всех поверхностях)',
  '#7aa2f7': 'акцент / middle',
  '#56d1b0': 'junior',
  '#a78bfa': 'senior',
  '#f08c4b': 'staff',
};

let bad = 0;
for (const [value, what] of Object.entries(shared)) {
  for (const [name, src] of [['tokens.css', app], ['styles.css (лендинг)', landing]]) {
    if (!src.includes(value)) {
      console.error(`${value} (${what}) отсутствует в ${name}`);
      bad++;
    }
  }
}
// Градиент 120deg был декором ради декора (заголовки-текстом и кнопки) — в списке антишаблонов.
if (/linear-gradient\(120deg/.test(landing)) {
  console.error('лендинг вернулся к градиенту-декору linear-gradient(120deg, …)');
  bad++;
}
if (bad > 0) {
  console.error(`токены разошлись: ${bad} расхождений`);
  process.exit(1);
}
console.log(`токены лендинга и приложения совпадают (${Object.keys(shared).length} значений)`);
