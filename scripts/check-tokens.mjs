#!/usr/bin/env node
/**
 * Палитра приложения (services/frontend/src/tokens.css) и лендинга (styles.css)
 * живёт в двух файлах: лендинг собирается без vite и не может сделать @import.
 * Проверка не даёт значениям разойтись — ровно из-за чего лендинг и кабинет
 * выглядели разными продуктами (замерено в браузере, ui-review 2026-11-24).
 *
 * Здесь же вторая проверка того же рода: var(--что-то) в index.css без
 * объявления не падает с ошибкой, а молча берёт подстановку, и правило
 * перестаёт применяться. Так у нас жили var(--muted, ...) — имя осталось со
 * времён, когда стили были общие с лендингом.
 */
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const appTokens = readFileSync(join(root, 'services/frontend/src/tokens.css'), 'utf8');
const landingCss = readFileSync(join(root, 'styles.css'), 'utf8');
const appCss = readFileSync(join(root, 'services/frontend/src/index.css'), 'utf8');
const app = appTokens.toLowerCase();
const landing = landingCss.toLowerCase();

// Значения, которые обязаны быть в обоих файлах.
const shared = {
  '#080b11': 'фон',
  '#0d111a': 'панель',
  '#131926': 'карточка',
  '#232c3b': 'линия',
  '#e3e8f2': 'текст',
  '#8f9bb0': 'подписи (AA на всех поверхностях)',
  '#7aa2f7': 'акцент (он же мидл на лендинге)',
  '#56d1b0': 'junior на лендинге, --live в приложении',
};

// Мастера грейдов, которые нужны только лендингу: ни один экран приложения
// грейды не красит (проверено по index.css и .ts/.tsx), и держать в токенах
// неиспользуемую шкалу — значит врать про то, как устроен цвет.
const landingOnly = {
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
for (const [value, what] of Object.entries(landingOnly)) {
  if (!landing.includes(value)) {
    console.error(`${value} (${what}) исчезло с лендинга`);
    bad++;
  }
}

// Градиент 120deg был декором ради декора (заголовки-текстом и кнопки) — в списке антишаблонов.
if (/linear-gradient\(120deg/.test(landing)) {
  console.error('лендинг вернулся к градиенту-декору linear-gradient(120deg, …)');
  bad++;
}

// Каждое var(--x) в index.css обязано быть объявлено — в токенах или локально
// в самом правиле. Объявления внутри секций законны, поэтому собираем их оттуда.
const declared = new Set();
for (const src of [appTokens, appCss]) {
  for (const m of src.matchAll(/(--[a-z0-9-]+)\s*:/g)) declared.add(m[1]);
}
const missing = [...new Set([...appCss.matchAll(/var\((--[a-z0-9-]+)/g)].map((m) => m[1]))]
  .filter((name) => !declared.has(name))
  .sort();
if (missing.length > 0) {
  console.error(`var() на необъявленный токен в index.css: ${missing.join(', ')}`);
  bad++;
}

if (bad > 0) {
  console.error(`токены разошлись: ${bad} расхождений`);
  process.exit(1);
}
console.log(
  `токены совпадают (${Object.keys(shared).length} общих значений, ` +
    `${Object.keys(landingOnly).length} только лендинговых, ` +
    'необъявленных var() нет)',
);
