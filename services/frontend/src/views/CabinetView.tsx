/**
 * Кабинет кандидата (WP-7): профиль + баланс минут,
 * «Новое интервью» (грейд + стек), история сессий со статусами.
 * Активные/паузные сессии ведут в голосовой интерфейс (WP-8) —
 * пока кнопка «Продолжить» ведёт в заглушку, завершённые — в транскрипт.
 */
import { useCallback, useEffect, useState, type FormEvent } from 'react';
import {
  createSession,
  listSessions,
  ApiError,
  type Grade,
  type Session,
  type Stack,
} from '../api';
import { useAuth } from '../auth';
import { apiErrorMessage } from './LoginView';
import { statusLabel, stageLabel } from '../labels';

const GRADES: { value: Grade; label: string; minutes: number }[] = [
  { value: 'junior', label: 'Junior (45 мин)', minutes: 45 },
  { value: 'middle', label: 'Middle (50 мин)', minutes: 50 },
  { value: 'senior', label: 'Senior (60 мин)', minutes: 60 },
  { value: 'staff', label: 'Staff (75 мин)', minutes: 75 },
];

const STACKS: { value: Stack; label: string }[] = [
  { value: 'go', label: 'Go' },
  { value: 'python', label: 'Python' },
];

/** Путь к странице сессии (hash-роутинг, App.tsx). */
export function sessionPath(id: number): string {
  return `#/sessions/${id}`;
}

export function CabinetView() {
  const { user, minutesRemainingMin, logout, refresh } = useAuth();
  const [sessions, setSessions] = useState<Session[] | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [grade, setGrade] = useState<Grade>('middle');
  const [stack, setStack] = useState<Stack>('go');
  const [starting, setStarting] = useState(false);
  const [startError, setStartError] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      setSessions(await listSessions());
      setLoadError(null);
    } catch (e) {
      setLoadError(apiErrorMessage(e));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const gradeMinutes = GRADES.find((g) => g.value === grade)?.minutes ?? 50;
  const notEnoughMinutes = minutesRemainingMin < gradeMinutes;

  const onStart = async (e: FormEvent) => {
    e.preventDefault();
    if (starting || notEnoughMinutes) return;
    setStarting(true);
    setStartError(null);
    try {
      const s = await createSession(grade, stack);
      window.location.hash = sessionPath(s.id);
    } catch (err) {
      setStartError(apiErrorMessage(err));
      if (err instanceof ApiError && err.code === 'no_minutes') {
        void refresh();
      }
    } finally {
      setStarting(false);
    }
  };

  return (
    <main className="app cabinet">
      <header className="header">
        <div>
          <h1>Грейд</h1>
          <p className="muted small">
            <span data-testid="user-email">{user?.email}</span> · минут:{' '}
            <strong data-testid="minutes">{minutesRemainingMin}</strong>
          </p>
        </div>
        <button type="button" className="btn ghost" onClick={logout}>
          Выйти
        </button>
      </header>

      <section className="card" aria-labelledby="new-interview">
        <h2 id="new-interview">Новое интервью</h2>
        <form className="start-form" onSubmit={onStart}>
          <label className="field">
            <span>Грейды</span>
            <select value={grade} onChange={(e) => setGrade(e.target.value as Grade)}>
              {GRADES.map((g) => (
                <option key={g.value} value={g.value}>
                  {g.label}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            <span>Стек</span>
            <select value={stack} onChange={(e) => setStack(e.target.value as Stack)}>
              {STACKS.map((s) => (
                <option key={s.value} value={s.value}>
                  {s.label}
                </option>
              ))}
            </select>
          </label>
          {notEnoughMinutes && (
            <p className="form-warn">
              Не хватает минут: для {grade} нужно {gradeMinutes}, доступно{' '}
              {minutesRemainingMin}.
            </p>
          )}
          {startError !== null && (
            <p className="form-error" role="alert">
              {startError}
            </p>
          )}
          <button
            type="submit"
            className="btn primary"
            disabled={starting || notEnoughMinutes}
            data-testid="start-session"
          >
            {starting ? 'Запуск…' : 'Начать интервью'}
          </button>
        </form>
      </section>

      <section className="card" aria-labelledby="history">
        <div className="history-head">
          <h2 id="history">История</h2>
          <button type="button" className="btn ghost small" onClick={() => void load()}>
            Обновить
          </button>
        </div>
        {loadError !== null && <p className="form-error">{loadError}</p>}
        {sessions === null && loadError === null && <p className="muted">Загрузка…</p>}
        {sessions !== null && sessions.length === 0 && (
          <p className="muted">Сессий пока нет — начните первое интервью.</p>
        )}
        {sessions !== null && sessions.length > 0 && (
          <table className="sessions-table">
            <thead>
              <tr>
                <th>Дата</th>
                <th>Грейд</th>
                <th>Стек</th>
                <th>Стадия</th>
                <th>Статус</th>
                <th>Длительность</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {sessions.map((s) => (
                <tr key={s.id} data-session-status={s.status}>
                  <td>{formatDate(s.started_at)}</td>
                  <td>{s.grade}</td>
                  <td>{s.stack}</td>
                  <td>{stageLabel(s.stage)}</td>
                  <td>
                    <span className={`status ${s.status}`}>{statusLabel(s.status)}</span>
                  </td>
                  <td>{formatSeconds(s.active_seconds)}</td>
                  <td>
                    {s.status === 'finished' || s.status === 'aborted' ? (
                      <a className="btn ghost small" href={sessionPath(s.id)}>
                        Транскрипт
                      </a>
                    ) : (
                      <a className="btn ghost small" href={sessionPath(s.id)}>
                        Продолжить
                      </a>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </main>
  );
}

function formatDate(iso: string): string {
  const d = new Date(iso);
  return d.toLocaleString('ru-RU', {
    day: '2-digit',
    month: '2-digit',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}

function formatSeconds(total: number): string {
  const m = Math.floor(total / 60);
  const s = Math.round(total % 60);
  return `${m} мин ${s.toString().padStart(2, '0')} с`;
}
