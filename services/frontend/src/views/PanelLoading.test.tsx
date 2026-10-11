import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { PanelLoading } from './PanelLoading';

describe('PanelLoading (B2: skeleton ленивого чанка)', () => {
  it('показывает спиннер и текст «Загрузка…» как Suspense-fallback', () => {
    render(<PanelLoading />);
    expect(screen.getByTestId('panel-loading')).toBeInTheDocument();
    // доступность: role=status + aria-live (экранные дикторы объявят смену)
    expect(screen.getByRole('status')).toHaveAttribute('aria-live', 'polite');
    expect(screen.getByRole('status').querySelector('.panel-loading__spinner')).not.toBeNull();
    expect(screen.getByText('Загрузка…')).toBeInTheDocument();
  });
});
