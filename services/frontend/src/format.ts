/**
 * Форматирование чисел. Даты в проекте печатаются по-русски (ru-RU), а баллы
 * через toFixed давали «3.75» с точкой — в одном экране два разделителя
 * (ui-review 2026-11-27).
 */
export function num(v: number, digits = 2): string {
  return new Intl.NumberFormat('ru-RU', {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  }).format(v);
}
