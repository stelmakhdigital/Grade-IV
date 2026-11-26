/**
 * Клавиатура для role=tablist из кнопок (ui-review 2026-11-27): без стрелок
 * вкладка с ролью tab — обещание, которое виджет не выполняет.
 *
 * Логика на контейнер: обработчик вешается на обёртку вкладок, активная
 * вкладка следует за фокусом.
 */
export function tablistKeyDown(opts: {
  index: number;
  count: number;
  onSelect: (i: number) => void;
}): (e: React.KeyboardEvent<HTMLElement>) => void {
  const { index, count, onSelect } = opts;
  return (e) => {
    if (count < 2) return;
    const step =
      e.key === 'ArrowRight' || e.key === 'ArrowDown'
        ? 1
        : e.key === 'ArrowLeft' || e.key === 'ArrowUp'
          ? -1
          : 0;
    const next =
      e.key === 'Home' ? 0 : e.key === 'End' ? count - 1 : step === 0 ? -1 : (index + step + count) % count;
    if (next < 0) return;
    e.preventDefault();
    onSelect(next);
    const box = e.currentTarget as HTMLElement;
    box.querySelectorAll<HTMLElement>('[role="tab"]')[next]?.focus();
  };
}
