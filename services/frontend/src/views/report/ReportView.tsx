/**
 * Отчёт по итогам интервью (WP-11): критерии §12 (шкала 1–5 с весами),
 * grade-рекомендация, сильные/слабые стороны, рекомендации.
 * 202 «generating» — опрос каждые 2 с (максимум ~1 мин).
 */
import { useCallback, useEffect, useState } from 'react';
import { getReport, type Report } from '../../api';
import { apiErrorMessage } from '../LoginView';

export interface ReportViewProps {
  sessionId: number;
  /** Интервал опроса 202, мс (тесты — быстрее). */
  pollMs?: number;
  /** Лимит опросов (тесты — меньше). */
  maxPolls?: number;
}

const POLL_MS = 2000;
const MAX_POLLS = 30;

export function ReportView({ sessionId, pollMs = POLL_MS, maxPolls = MAX_POLLS }: ReportViewProps) {
  const [report, setReport] = useState<Report | null>(null);
  const [state, setState] = useState<'loading' | 'ready' | 'error'>('loading');
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async (pollsLeft: number) => {
    try {
      const r = await getReport(sessionId);
      if (r === 'generating') {
        if (pollsLeft > 1) {
          setState('loading');
          window.setTimeout(() => {
            void load(pollsLeft - 1);
          }, pollMs);
          return;
        }
        // генерация затянулась — повторить вручную
        setState('error');
        setError('Отчёт ещё генерируется. Обновите страницу через минуту.');
        return;
      }
      setReport(r);
      setState('ready');
    } catch (e) {
      setState('error');
      setError(apiErrorMessage(e));
    }
  }, [sessionId]);

  useEffect(() => {
    setState('loading');
    void load(maxPolls);
  }, [load, maxPolls]);

  if (state === 'loading') {
    return (
      <section className="card report" data-testid="report-loading">
        <p className="muted">Считаем результаты интервью…</p>
      </section>
    );
  }

  if (state === 'error' || report === null) {
    return (
      <section className="card report" data-testid="report-error">
        <p className="form-error" role="alert">{error}</p>
        <button type="button" className="btn ghost" onClick={() => void load(MAX_POLLS)}>
          Повторить
        </button>
      </section>
    );
  }

  const scoreColor = (s: number): string => (s >= 4 ? 'good' : s >= 2.5 ? 'mid' : 'bad');

  return (
    <section className="card report" data-testid="report">
      <header className="report-head">
        <div>
          <h2>Итог: {report.overall.toFixed(2)} / 5</h2>
          <p className="report-grade" data-testid="report-grade">
            {report.grade_recommendation}
          </p>
          {report.verdict !== undefined && report.verdict !== '' && (
            <p className="report-verdict" data-testid="report-verdict">
              {report.verdict}
            </p>
          )}
        </div>
      </header>

      <h3>Критерии</h3>
      <ul className="criteria" data-testid="criteria">
        {report.criteria.map((c) => (
          <li key={c.name} className="criterion">
            <div className="criterion-top">
              <span className="criterion-name">{c.name}</span>
              <span className={`criterion-score ${scoreColor(c.score)}`} data-testid={`score-${c.name}`}>
                {c.score.toFixed(1)}
                <small className="muted"> · {Math.round(c.weight * 100)}%</small>
              </span>
            </div>
            <div className="criterion-bar" aria-hidden>
              <div
                className={`criterion-fill ${scoreColor(c.score)}`}
                style={{ width: `${Math.min(100, (c.score / 5) * 100)}%` }}
              />
            </div>
            {c.comment !== undefined && c.comment !== '' && (
              <p className="muted">{c.comment}</p>
            )}
            {c.evidence && c.evidence.length > 0 && (
              <div className="criterion-evidence" data-testid={`evidence-${c.name}`}>
                <strong>Примеры из интервью:</strong>
                <ul>{c.evidence.map((e, i) => <li key={i}>{e}</li>)}</ul>
              </div>
            )}
            {c.gap_to_grade !== undefined && c.gap_to_grade > 0 && (
              <p className="criterion-gap" data-testid={`gap-${c.name}`}>
                Разрыв до грейда: {c.gap_to_grade.toFixed(1)} балла
              </p>
            )}
            {c.study_plan && c.study_plan.length > 0 && (
              <div className="criterion-study">
                <strong>Что подтянуть:</strong>
                <ul>{c.study_plan.map((s, i) => <li key={i}>{s}</li>)}</ul>
              </div>
            )}
          </li>
        ))}
      </ul>

      <div className="report-columns">
        <div>
          <h3>Сильные стороны</h3>
          <ul>{report.strengths.map((s, i) => <li key={i}>{s}</li>)}</ul>
        </div>
        <div>
          <h3>Зоны роста</h3>
          <ul>{report.weaknesses.map((s, i) => <li key={i}>{s}</li>)}</ul>
        </div>
      </div>

      <h3>Рекомендации</h3>
      <ol data-testid="recommendations">
        {report.recommendations.map((s, i) => <li key={i}>{s}</li>)}
      </ol>

      {report.grade_gap !== undefined && report.grade_gap !== '' && (
        <section className="report-grade-gap" data-testid="report-grade-gap">
          <h3>Разрыв до целевого грейда</h3>
          <p>{report.grade_gap}</p>
        </section>
      )}

      {report.study_plan_2weeks && report.study_plan_2weeks.length > 0 && (
        <section className="report-study-2w" data-testid="report-study-2w">
          <h3>План подготовки на 2 недели</h3>
          <ol>{report.study_plan_2weeks.map((s, i) => <li key={i}>{s}</li>)}</ol>
        </section>
      )}

      {report.progress_vs_previous && report.progress_vs_previous.length > 0 && (
        <section className="report-progress" data-testid="report-progress">
          <h3>Прогресс по прошлым интервью</h3>
          <table className="progress-table">
            <thead>
              <tr>
                <th>Дата</th>
                <th>Грейд</th>
                <th>Стек</th>
                <th>Итог</th>
                <th>Δ к текущему</th>
              </tr>
            </thead>
            <tbody>
              {report.progress_vs_previous.map((p) => (
                <tr key={p.session_id} className={p.same_stack ? 'same-stack' : ''}>
                  <td>{new Date(p.date).toLocaleDateString('ru-RU')}</td>
                  <td>{p.grade}</td>
                  <td>{p.stack}</td>
                  <td data-testid={`progress-overall-${p.session_id}`}>{p.overall.toFixed(2)}</td>
                  <td>
                    {Object.entries(p.criteria_delta).map(([k, d]) => (
                      <span key={k} className={`delta ${d > 0 ? 'up' : d < 0 ? 'down' : 'flat'}`}>
                        {k}: {d > 0 ? '+' : ''}{d.toFixed(1)}
                      </span>
                    ))}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          <p className="muted small">Выделенные строки — интервью на том же стеке.</p>
        </section>
      )}
    </section>
  );
}
