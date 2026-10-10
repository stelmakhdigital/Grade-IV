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

  it('итерация B: verdict, evidence, gap, study plan, 2-недельный план, прогресс', async () => {
    const REPORT_B = {
      ...REPORT,
      verdict: 'Сильное Middle, уверенно в алгоритмах.',
      criteria: [
        {
          name: 'Live-Code (алгоритм, качество кода, тесты)',
          weight: 0.25,
          score: 3,
          comment: 'нужна практика',
          evidence: ['решил задачу за 15 мин', 'пропущен edge-case с пустым вводом'],
          gap_to_grade: 1.5,
          study_plan: ['разбирать 2 задачи в день', 'учить хеш-таблицы'],
        },
        { name: 'Коммуникация (ясность, структура, русский язык)', weight: 0.15, score: 4 },
      ],
      study_plan_2weeks: ['неделя 1: алгоритмы', 'неделя 2: system design'],
      grade_gap: 'До Senior не хватает системного дизайна.',
      progress_vs_previous: [
        { session_id: 1, date: '2026-09-14T10:00:00Z', stack: 'go', grade: 'middle', overall: 3.8, same_stack: true, criteria_delta: { 'Live-Code (алгоритм, качество кода, тесты)': -0.8 } },
        { session_id: 2, date: '2026-09-20T10:00:00Z', stack: 'python', grade: 'junior', overall: 4.1, same_stack: false, criteria_delta: {} },
      ],
    };
    vi.stubGlobal('fetch', vi.fn(async () => json(200, REPORT_B)));
    render(<ReportView sessionId={9} />);
    expect(await screen.findByTestId('report')).toBeInTheDocument();
    expect(screen.getByTestId('report-verdict')).toHaveTextContent('Сильное Middle');
    expect(screen.getByTestId('evidence-Live-Code (алгоритм, качество кода, тесты)')).toHaveTextContent('пропущен edge-case');
    expect(screen.getByTestId('gap-Live-Code (алгоритм, качество кода, тесты)')).toHaveTextContent('1.5');
    expect(screen.getByText('разбирать 2 задачи в день')).toBeInTheDocument();
    expect(screen.getByTestId('report-study-2w')).toHaveTextContent('неделя 1: алгоритмы');
    expect(screen.getByTestId('report-grade-gap')).toHaveTextContent('До Senior не хватает');
    expect(screen.getByTestId('report-progress')).toBeInTheDocument();
    expect(screen.getByTestId('progress-overall-1')).toHaveTextContent('3.80');
    expect(screen.getByTestId('progress-overall-2')).toHaveTextContent('4.10');
    // Δ к текущему
    expect(screen.getByText('Live-Code (алгоритм, качество кода, тесты): -0.8')).toBeInTheDocument();
  });
});
