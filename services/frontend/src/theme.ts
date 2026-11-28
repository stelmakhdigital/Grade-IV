/**
 * Тема интерфейса: тёмная (историческая) и светлая. По умолчанию — системная
 * (prefers-color-scheme), переключатель в шапке закрепляет выбор в localStorage.
 *
 * Механика намеренно примитивная: один атрибут на <html> + `color-scheme`,
 * палитра целиком живёт в tokens.css (`[data-theme='light']`). Никакого
 * провайдера и контекста: тема не участвует в рендере, читать её из компонентов
 * нужно только переключателю.
 */
export type Theme = 'light' | 'dark';

const KEY = 'grade-theme';

/** Выбор системы (когда пользователь ничего не выбирал сам). */
export function systemTheme(): Theme {
  return typeof matchMedia === 'function' && matchMedia('(prefers-color-scheme: light)').matches
    ? 'light'
    : 'dark';
}

/** Ручной выбор или null — «как в системе». */
export function storedTheme(): Theme | null {
  const v = localStorage.getItem(KEY);
  return v === 'light' || v === 'dark' ? v : null;
}

export function currentTheme(): Theme {
  return storedTheme() ?? systemTheme();
}

export function applyTheme(t: Theme): void {
  const root = document.documentElement;
  root.dataset.theme = t;
  // color-scheme через стиль: нативные скроллбары, ползунки и календари
  // подхватывают тему сразу, а не только там, где её успел перекрасить CSS.
  root.style.colorScheme = t;
}

/** null — вернуть «как в системе». */
export function setTheme(t: Theme | null): Theme {
  if (t === null) localStorage.removeItem(KEY);
  else localStorage.setItem(KEY, t);
  const effective = t ?? systemTheme();
  applyTheme(effective);
  return effective;
}

/**
 * Следить за системой, пока выбор не сделан вручную: иначе стенд в тёмной
 * системе открывается светлым намертво, а «авто» обещает следовать системе.
 */
export function watchSystemTheme(): () => void {
  if (typeof matchMedia !== 'function') return () => {};
  const mq = matchMedia('(prefers-color-scheme: light)');
  const onChange = () => {
    if (storedTheme() === null) applyTheme(systemTheme());
  };
  mq.addEventListener?.('change', onChange);
  return () => mq.removeEventListener?.('change', onChange);
}
