/**
 * Цвета блоков палитры System Design (ADR-004).
 * Детерминированная связь «имя блока → цвет»: палитра и вставленный на холст
 * блок одного цвета (раньше цвет блока — Math.random, палитры-связи не было).
 */
export const PALETTE_COLORS = [
  'hsl(210, 90%, 70%)', // синий — трафик
  'hsl(45, 95%, 60%)', // жёлтый — сервисы
  'hsl(150, 70%, 55%)', // зелёный — данные
  'hsl(0, 80%, 65%)', // красный — внешнее/аварии
  'hsl(275, 75%, 65%)', // фиолетовый — инфраструктура
  'hsl(330, 75%, 60%)', // розовый — прочее
] as const;

// Сумма кодов — простой детерминированный хэш (стабильно между рендерами и
// перезапусками; качество хэша для 12 имён не критично).
function hashName(name: string): number {
  let h = 0;
  for (let i = 0; i < name.length; i++) {
    h = (h * 31 + name.charCodeAt(i)) >>> 0;
  }
  return h;
}

/** Цвет для блока палитры (детерминированный по имени). */
export function blockColor(name: string): string {
  return PALETTE_COLORS[hashName(name) % PALETTE_COLORS.length] ?? PALETTE_COLORS[0];
}
