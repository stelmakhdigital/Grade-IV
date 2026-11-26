import { describe, expect, it, vi } from 'vitest';
//_guard_ патчит window.addEventListener на старте — как в браузере (main.tsx
// импортирует его раньше всего остального).
import './unload-guard';

describe('unload-guard', () => {
  it('unload-обработчик не ставится', () => {
    const fn = vi.fn();
    window.addEventListener('unload', fn);
    window.dispatchEvent(new Event('unload'));
    expect(fn).not.toHaveBeenCalled();
  });

  it('прочие события window работают как обычно', () => {
    const fn = vi.fn();
    window.addEventListener('resize', fn);
    window.dispatchEvent(new Event('resize'));
    expect(fn).toHaveBeenCalledTimes(1);
    window.removeEventListener('resize', fn);
  });

  it('обработчик на элементе (не window) не трогается', () => {
    const el = document.createElement('div');
    const fn = vi.fn();
    el.addEventListener('click', fn);
    el.click();
    expect(fn).toHaveBeenCalledTimes(1);
  });
});
