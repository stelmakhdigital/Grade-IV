import { describe, expect, it, vi } from 'vitest';
import { tablistKeyDown } from './tablistKeys';

/** Клавиатура role=tablist (ui-review 2026-11-27). */
function press(key: string, opts: { index: number; count: number }) {
  const onSelect = vi.fn();
  const focus = vi.fn();
  const tab = { focus };
  const box = { querySelectorAll: () => Array.from({ length: opts.count }, () => tab) } as unknown as HTMLElement;
  const e = {
    key,
    currentTarget: box,
    preventDefault: vi.fn(),
  } as unknown as React.KeyboardEvent<HTMLElement>;
  tablistKeyDown({ ...opts, onSelect })(e);
  return { onSelect, focus, e };
}

describe('tablistKeyDown', () => {
  it('ArrowRight переводит на следующую вкладку и фокусит её', () => {
    const { onSelect, focus, e } = press('ArrowRight', { index: 0, count: 3 });
    expect(onSelect).toHaveBeenCalledWith(1);
    expect(focus).toHaveBeenCalled();
    expect(e.preventDefault).toHaveBeenCalled();
  });

  it('ArrowLeft с первой уходит на последнюю (циклически)', () => {
    const { onSelect } = press('ArrowLeft', { index: 0, count: 3 });
    expect(onSelect).toHaveBeenCalledWith(2);
  });

  it('Home/End — к краям', () => {
    expect(press('Home', { index: 1, count: 3 }).onSelect).toHaveBeenCalledWith(0);
    expect(press('End', { index: 1, count: 3 }).onSelect).toHaveBeenCalledWith(2);
  });

  it('посторонняя клавиша ничего не делает и не гасит событие', () => {
    const { onSelect, e } = press('a', { index: 0, count: 3 });
    expect(onSelect).not.toHaveBeenCalled();
    expect(e.preventDefault).not.toHaveBeenCalled();
  });

  it('на одних вкладках стрелки молчат', () => {
    const { onSelect } = press('ArrowRight', { index: 0, count: 1 });
    expect(onSelect).not.toHaveBeenCalled();
  });
});
