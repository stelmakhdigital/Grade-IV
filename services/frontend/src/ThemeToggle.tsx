/**
 * Переключатель темы в шапке. Две кнопки, а не одна-гадалка: «как в системе» —
 * отдельное состояние, и человек должен видеть, где он сейчас (выбор вручную
 * перебивает систему и закрепляется).
 */
import { useState } from 'react';
import { currentTheme, setTheme, storedTheme, type Theme } from './theme';

const OPTIONS: { value: Theme; label: string }[] = [
  { value: 'dark', label: 'Тёмная' },
  { value: 'light', label: 'Светлая' },
];

export function ThemeToggle() {
  const [theme, setLocal] = useState<Theme>(currentTheme());
  const auto = storedTheme() === null;

  return (
    <div className="theme-toggle" role="group" aria-label="Тема интерфейса">
      {OPTIONS.map((o) => (
        <button
          key={o.value}
          type="button"
          className={theme === o.value ? 'theme-btn active' : 'theme-btn'}
          aria-pressed={theme === o.value}
          title={
            o.value === 'light'
              ? 'Светлая тема (при ярком экране и в проекторе)'
              : 'Тёмная тема'
          }
          onClick={() => setLocal(setTheme(o.value))}
        >
          {o.label}
        </button>
      ))}
      {!auto && (
        <button
          type="button"
          className="theme-btn ghost"
          title="Сбросить выбор и следовать системе"
          onClick={() => setLocal(setTheme(null))}
        >
          Авто
        </button>
      )}
    </div>
  );
}
