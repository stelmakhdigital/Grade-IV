import { render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ReportView } from './ReportView';

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status });
}

const REPORT = {
  overall: 4.25,
  grade_recommendation: 'грейд подтверждён с запасом',
  criteria: [
    { name: 'Live-Code (алгоритм, качество кода, тесты)', weight: 0.25, score: 5, comment: 'отлично' },
    { name: 'Коммуникация (ясность, структура, русский язык)', weight: 0.15, score: 4 },
  ],
  strengths: ['сильная аргументация'],
  weaknesses: ['мало примеров'],
  recommendations: ['попрактиковаться в design'],
};

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('ReportView (WP-11)', () => {
  it('200: рендер итогового отчёта', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => json(200, REPORT)));
    render(<ReportView sessionId={9} />);
    expect(await screen.findByTestId('report')).toBeInTheDocument();
    expect(screen.getByTestId('report')).toHaveTextContent('Итог: 4.25 / 5');
    expect(screen.getByTestId('report-grade')).toHaveTextContent(
      'грейд подтверждён с запасом',
    );
    expect(screen.getByTestId('score-Live-Code (алгоритм, качество кода, тесты)')).toHaveTextContent(
      '5.0',
    );
    expect(screen.getByText('сильная аргументация')).toBeInTheDocument();
    expect(screen.getByText('попрактиковаться в design')).toBeInTheDocument();
  });

  it('202 → 200: опрос до готовности', async () => {
    let calls = 0;
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => {
        calls += 1;
        return calls < 3 ? json(202, { status: 'generating' }) : json(200, REPORT);
      }),
    );
    render(<ReportView sessionId={9} pollMs={10} maxPolls={5} />);
    // опросы каждые 10 мс: 202, 202, 200
    await waitFor(
      () => expect(screen.getByTestId('report')).toBeInTheDocument(),
      { timeout: 3000 },
    );
    expect(calls).toBeGreaterThanOrEqual(3);
  });

  it('409: ошибка (сессия не завершена)', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        json(409, { code: 'invalid_state', message: 'отчёт доступен после завершения сессии' }),
      ),
    );
    render(<ReportView sessionId={9} />);
    await waitFor(() => {
      expect(screen.getByTestId('report-error')).toHaveTextContent(
        'отчёт доступен после завершения сессии',
      );
    });
  });

  it('ошибка «ещё генерируется» после исчерпания опросов — с кнопкой «Повторить»', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => json(202, { status: 'generating' })),
    );
    render(<ReportView sessionId={9} pollMs={10} maxPolls={4} />);
    // 4 опроса × 10 мс — исчерпано, «ещё генерируется»
    await waitFor(() =>
      expect(screen.getByTestId('report-error')).toHaveTextContent(
        'Отчёт ещё генерируется',
      ),
    );
  });
});
