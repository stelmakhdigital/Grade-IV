/**
 * Страница сессии (WP-7 — базовая часть): шапка с мета-данными,
 * список событий (транскрипт) из /sessions/{id}/events.
 * Голосовой интерфейс (WS) — WP-8, Live-Code UI — WP-9, System Design — WP-10:
 * для незавершённых сессий показывается заглушка.
 */
import { useEffect, useState } from 'react';
import {
  getSession,
  listEvents,
  type Session,
  type SessionEvent,
} from '../api';
import { useAuth } from '../auth';
import { apiErrorMessage } from './LoginView';
import { eventKindLabel, eventText, statusLabel, stageLabel } from '../labels';

export function TranscriptView({ id }: { id: number }) {
  const { user } = useAuth();
  const [session, setSession] = useState<Session | null>(null);
  const [events, setEvents] = useState<SessionEvent[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let alive = true;
    (async () => {
      try {
        const [s, ev] = await Promise.all([getSession(id), listEvents(id)]);
        if (!alive) return;
        setSession(s);
        setEvents(ev);
      } catch (e) {
        if (alive) setError(apiErrorMessage(e));
      }
    })();
    return () => {
      alive = false;
    };
  }, [id]);

  if (error !== null) {
    return (
      <main className="app">
        <header className="header">
          <h1>Грейд</h1>
          <a className="btn ghost" href="#/">Кабинет</a>
        </header>
        <p className="form-error" role="alert">
          {error}
        </p>
      </main>
    );
  }
  if (session === null || events === null) {
    return (
      <main className="app">
        <h1>Грейд</h1>
        <p className="muted">Загрузка сессии…</p>
      </main>
    );
  }

  return (
    <main className="app session-page">
      <header className="header">
        <div>
          <h1>
            Интервью: {session.grade} / {session.stack}
          </h1>
          <p className="muted small">
            {user?.email} · {stageLabel(session.stage)} ·{' '}
            <span className={`status ${session.status}`}>{statusLabel(session.status)}</span>
          </p>
        </div>
        <a className="btn ghost" href="#/">
          В кабинет
        </a>
      </header>

      {(session.status === 'active' || session.status === 'paused') && (
        <section className="card notice">
          <p>
            Голосовой интерфейс сессии готовится (следующий этап). Пока можно
            ознакомиться с записью диалога ниже.
          </p>
        </section>
      )}

      <section className="card transcript" aria-label="Транскрипт">
        {events.length === 0 && <p className="muted">Событий пока нет.</p>}
        <ol className="transcript-list">
          {events.map((ev) => {
            const text = eventText(ev.kind, ev.data);
            return (
              <li key={ev.seq} className={`event ${ev.kind}`}>
                <div className="event-head">
                  <span className="event-kind">{eventKindLabel(ev.kind)}</span>
                  <time>{formatTs(ev.ts)}</time>
                </div>
                {text !== '' && <div className="event-text">{text}</div>}
              </li>
            );
          })}
        </ol>
      </section>
    </main>
  );
}

function formatTs(iso: string): string {
  return new Date(iso).toLocaleTimeString('ru-RU', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
}
