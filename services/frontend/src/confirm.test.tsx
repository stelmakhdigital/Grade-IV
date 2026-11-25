import { fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { ConfirmDialog } from './confirm';

describe('ConfirmDialog', () => {
  it('подтверждает один раз и закрывается', () => {
    const run = vi.fn();
    const onCancel = vi.fn();
    render(<ConfirmDialog ask={{ question: 'Удалить шаблон «Go middle»? ', run }} onCancel={onCancel} />);
    expect(screen.getByRole('dialog')).toBeInTheDocument();

    fireEvent.click(screen.getByTestId('confirm-yes'));
    expect(run).toHaveBeenCalledTimes(1);
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it('«Отмена» не выполняет действие', () => {
    const run = vi.fn();
    render(<ConfirmDialog ask={{ question: '?', run }} onCancel={vi.fn()} />);
    fireEvent.click(screen.getByRole('button', { name: 'Отмена' }));
    expect(run).not.toHaveBeenCalled();
  });

  it('Escape отменяет, фокус уходит внутрь диалога', async () => {
    const run = vi.fn();
    const onCancel = vi.fn();
    const user = userEvent.setup();
    render(<ConfirmDialog ask={{ question: '?', run }} onCancel={onCancel} />);
    // useDialog при открытии ведёт фокус на первый элемент внутри
    expect(screen.getByRole('button', { name: 'Закрыть' })).toHaveFocus();
    await user.keyboard('{Escape}');
    expect(onCancel).toHaveBeenCalledTimes(1);
    expect(run).not.toHaveBeenCalled();
  });
});
