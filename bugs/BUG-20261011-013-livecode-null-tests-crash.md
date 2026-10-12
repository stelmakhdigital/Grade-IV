# BUG-20261011-013 · Live-Code: краш панели (и размонтирование сессии) при tests=null

- **Статус:** исправлено
- **Область:** `services/frontend/src/api.ts` (`runTests`), `services/frontend/src/views/livecode/LiveCodePanel.tsx`

## Симптом
После «Запустить тесты» панель Live-Code падает с
`TypeError: Cannot read properties of null (reading 'filter')` в `<LiveCodePanel>`,
React размонтирует всё дерево сессии — исчезают кнопки стадий, отчёт, вся навигация.
Кандидат застревает на битой странице.

## Шаги воспроизведения
1. Любая ситуация, когда backend возвращает `tests: null`: compile-ошибка кода,
   `go: not found` в сандбоксе (см. BUG-20261011-014), setup-fail.
2. В панели «Запустить тесты» → ответ `200` с `"tests": null` → краш.

## Корень
Go-нильский слайш `Tests` маршалируется в JSON `null` (не `[]`), когда тесты не были
запущены (build-fail и т.п.). Фронтенд без защиты обращался `result.tests.filter(...)`
(useMemo) и `result.tests.length` (render) → TypeError при рендере.

## Фикс
- `api.ts runTests`: нормализация ответа — `{ ...r, tests: r.tests ?? [] }`
  (единственная точка входа REST-результата; WS `run_result` в панель результат не
  кладёт, только системную строку).
- `LiveCodePanel`: двойная защита — `(result.tests ?? [])` в `testStats` и в render
  списка тестов.

## Проверка
- tsc exit 0, vitest 16 files / 114 passed.
- Live: POST /runs с `tests: null` (некомпилируемый код) — панель не падает,
  показывает stderr/вывод и «Тесты: 0/0»-состояние без краша.
