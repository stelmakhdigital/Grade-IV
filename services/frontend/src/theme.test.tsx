/**
 * Тема: по умолчанию системная, переключатель закрепляет выбор, «Авто»
 * возвращает следование системе.
 */
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ThemeToggle } from './ThemeToggle';
import { applyTheme, currentTheme, setTheme, storedTheme } from './theme';

/** Система пользователя: matchMedia в jsdom нет, а тема на нём держится. */
function system(prefersLight: boolean) {
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches: prefersLight && query.includes('light'),
    addEventListener: () => {},
    removeEventListener: () => {},
  }));
}

beforeEach(() => {
  localStorage.clear();
  document.documentElement.removeAttribute('data-theme');
  system(false);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('theme', () => {
  it('без выбора — системная тема, и атрибут на <html> поставлен', () => {
    expect(currentTheme()).toBe('dark');
    system(true);
    expect(currentTheme()).toBe('light');
    expect(storedTheme()).toBeNull();

    applyTheme('light');
    expect(document.documentElement.dataset.theme).toBe('light');
    // color-scheme через стиль: нативные контролы (скроллбары, ползунки)
    // должны подхватить тему, а не остаться тёмными на бумаге.
    expect(document.documentElement.style.colorScheme).toBe('light');
  });

  it('ручной выбор перебивает систему и живёт после перезагрузки', () => {
    setTheme('light');
    system(false);
    expect(currentTheme()).toBe('light');
    expect(localStorage.getItem('grade-theme')).toBe('light');
    expect(setTheme(null)).toBe('dark');
    expect(localStorage.getItem('grade-theme')).toBeNull();
  });
});

describe('ThemeToggle', () => {
  it('переключает тему и показывает «Авто» только после ручного выбора', async () => {
    applyTheme(currentTheme());
    render(<ThemeToggle />);
    expect(screen.getByRole('button', { name: 'Тёмная' })).toHaveAttribute('aria-pressed', 'true');
    expect(screen.queryByRole('button', { name: 'Авто' })).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: 'Светлая' }));
    expect(document.documentElement.dataset.theme).toBe('light');
    expect(storedTheme()).toBe('light');
    expect(screen.getByRole('button', { name: 'Авто' })).toBeInTheDocument();

    system(true);
    await userEvent.click(screen.getByRole('button', { name: 'Авто' }));
    expect(document.documentElement.dataset.theme).toBe('light');
    expect(storedTheme()).toBeNull();

    system(false);
    await userEvent.click(screen.getByRole('button', { name: 'Авто' }));
    expect(document.documentElement.dataset.theme).toBe('dark');
  });
});
