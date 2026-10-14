/**
 * Публичная страница отчёта (F1-export, spec-16): /share/{token} без
 * аутентификации. Рендерит тот же ReportBody, что и кабинет, но без
 * персональных данных пользователя (email/ID наружу не выдаются).
 */
import { useEffect, useState } from 'react';
import { fetchPublicReport, type PublicReport } from '../../api';
import { apiErrorMessage } from '../LoginView';
import { ReportBody } from './ReportView';

export function ShareReportView({ token }: { token: string }) {
  const [report, setReport] = useState<PublicReport | null>(null);
  const [state, setState] = useState<'loading' | 'ready' | 'error'>('loading');
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let alive = true;
    setState('loading');
    fetchPublicReport(token)
      .then((r) => {
        if (!alive) return;
        setReport(r);
        setState('ready');
      })
      .catch((e: unknown) => {
        if (!alive) return;
        setState('error');
        setError(apiErrorMessage(e));
      });
    return () => {
      alive = false;
    };
  }, [token]);

  if (state === 'loading') {
    return (
      <main className="app">
        <h1>Грейд</h1>
        <section className="card report" data-testid="report-loading">
          <p className="muted">Загрузка отчёта…</p>
        </section>
      </main>
    );
  }

  if (state === 'error' || report === null) {
    return (
      <main className="app">
        <h1>Грейд</h1>
        <section className="card report" data-testid="report-error">
          <p className="form-error" role="alert">{error ?? 'Ссылка недоступна'}</p>
          <p className="muted">Публичные ссылки работают, пока владелец не отозвал их.</p>
        </section>
      </main>
    );
  }

  const metaChip = (
    <div className="report-share" data-testid="share-meta">
      <span className="muted small">
        Интервью: {report.meta.grade} · {report.meta.stack}
        {report.meta.finished_at ? ` · ${report.meta.finished_at}` : ''}
      </span>
      <span className="muted small">публичная ссылка</span>
    </div>
  );

  return (
    <main className="app">
      <h1>Грейд — отчёт по интервью</h1>
      <ReportBody report={report} headerExtra={metaChip} />
      <p className="muted small">
        Отчёт предоставлен по публичной ссылке (без аутентификации). Владелец может отозвать её в кабинете.
      </p>
    </main>
  );
}
