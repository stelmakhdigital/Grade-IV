import { render, screen } from '@testing-library/react';
import { App } from './App';

describe('App (каркас WP-1)', () => {
  it('рендерит заголовок и показывает статус API', async () => {
    globalThis.fetch = vi.fn().mockRejectedValue(new Error('no network'));
    render(<App />);
    expect(await screen.findByText('Грейд')).toBeInTheDocument();
    expect(await screen.findByText('api: недоступен')).toBeInTheDocument();
  });
});
