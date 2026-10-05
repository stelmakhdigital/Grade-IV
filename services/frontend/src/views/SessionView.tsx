/**
 * Страница сессии (WP-8): голосовое интервью.
 * Чистый UI: вся логика (WS, микрофон, плеер, таймер, транскрипт) — в
 * useVoiceSession. Рендер: активная сессия (голос/Live-Code/Design),
 * завершённая (запись диалога + отчёт).
 */
import { useAuth } from '../auth';
import { MicVisualizer, type MicEqMode } from './MicVisualizer';
import { useVoiceSession } from '../hooks/useVoiceSession';
import { statusLabel, stageLabel } from '../labels';
import { LiveCodePanel } from './livecode/LiveCodePanel';
import { DesignPanel } from './design/DesignPanel';
import { ReportView } from './report/ReportView';

export function SessionView({ id }: { id: number }) {
  const { user } = useAuth();
  const {
    session,
    loadError,
    lines,
    stage,
    runReview,
    designReview,
    task,
    remainingS,
    lastAiText,
    mic,
    speaking,
    wsState,
    error,
    micLevelRef,
    live,
    toggleMic,
    onStageAction,
    onFinish,
  } = useVoiceSession(id);

  if (loadError !== null) {
    return (
      <main className="app">
        <header className="header">
          <h1>Грейд</h1>
          <a className="btn ghost" href="#/">Кабинет</a>
        </header>
        <p className="form-error" role="alert">{loadError}</p>
      </main>
    );
  }
  if (session === null) {
    return (
      <main className="app">
        <h1>Грейд</h1>
        <p className="muted">Загрузка сессии…</p>
      </main>
    );
  }

  return (
    <main className="app session-page">
      <header className="header session-head">
        <div>
          <h1>
            Интервью: {session.grade} / {session.stack}
          </h1>
          <p className="muted small">
            {user?.email} · <span className={`status ${session.status}`}>{statusLabel(session.status)}</span>
            {live && remainingS !== null && (
              <span data-testid="timer"> · осталось {formatClock(remainingS)}</span>
            )}
            {live && (
              <span> · {stageLabel(stage)}</span>
            )}
          </p>
        </div>
        <div className="head-actions">
          {live && (
            <button type="button" className="btn ghost small" onClick={onFinish}>
              Завершить интервью
            </button>
          )}
          <a className="btn ghost small" href="#/">В кабинет</a>
        </div>
      </header>

      {live && (
        <section className="card voice-panel" aria-label="Голосовая сессия">
          <div className="voice-status">
            <span className={`conn ${wsState}`}>
              {wsState === 'open' ? 'соединение: активно'
                : wsState === 'connecting' ? 'подключение…'
                : wsState === 'error' ? 'ошибка соединения'
                : 'соединение закрыто (сессия на паузе)'}
            </span>
            {speaking && <span className="saying">ИИ говорит — чтобы ответить, дождитесь паузы</span>}
            {mic === 'running' && <span className="listening">микрофон: включён</span>}
            {mic === 'muted' && (
              <span className="form-error" role="alert" data-testid="mic-muted">
                Микрофон молчит — проверьте устройство, мьют и разрешения браузера.
              </span>
            )}
            {mic !== 'running' && mic !== 'denied' && mic !== 'muted' && stage === 'voice' && wsState === 'open' && (
              <span className="form-error" role="alert">
                Микрофон выключен — ИИ вас не слышит. Нажмите «Включить микрофон».
              </span>
            )}
          </div>

          {stage === 'livecode' && session !== null ? (
            <LiveCodePanel
              sessionId={id}
              stack={session.stack}
              taskId={task?.id}
              taskTitle={task?.title}
              taskFiles={task?.files ?? null}
              review={runReview}
            />
          ) : stage === 'design' ? (
            <DesignPanel sessionId={id} review={designReview} />
          ) : (
            <>
          {task !== null && task.statement !== undefined && (
            <div className="task-box">
              <strong>{task.title ?? task.id}</strong>
              <p>{task.statement}</p>
            </div>
          )}

          {lastAiText !== '' && (
            <p className="ai-say" data-testid="ai-say">
              {lastAiText}
            </p>
          )}

          <div className="voice-controls">
            {(() => {
              const eqMode: MicEqMode = mic !== 'running' ? 'idle' : speaking ? 'muted' : 'live';
              return <MicVisualizer mode={eqMode} levelRef={micLevelRef} />;
            })()}
            {mic === 'denied' && (
              <span className="form-error">Нет доступа к микрофону.</span>
            )}
            <button
              type="button"
              className={mic === 'running' || mic === 'muted' ? 'btn danger' : 'btn primary'}
              onClick={() => void toggleMic()}
              data-testid="mic-toggle"
              disabled={wsState !== 'open'}
            >
              {mic === 'running' || mic === 'muted' ? 'Выключить микрофон' : 'Включить микрофон'}
            </button>
            {stage === 'voice' && (
              <button type="button" className="btn ghost" onClick={() => onStageAction('livecode')}>
                К Live-Code
              </button>
            )}
            {stage === 'livecode' && (
              <button type="button" className="btn ghost" onClick={() => onStageAction('design')}>
                К System Design
              </button>
            )}
          </div>
            </>
          )}
          {error !== null && (
            <p className="form-error" role="alert">{error}</p>
          )}
        </section>
      )}

      {!live && (
        <>
        <section className="card notice">
          <p>
            Интервью {statusLabel(session.status)}. Ниже — запись диалога и отчёт.
          </p>
        </section>
        <ReportView sessionId={id} />
        </>
      )}

      <section className="card transcript" aria-label="Транскрипт">
        {lines.length === 0 && <p className="muted">Диалог появится здесь.</p>}
        <ol className="transcript-list" data-testid="lines">
          {lines.map((l, i) => (
            <li key={i} className={`line ${l.who}`}>
              <div className="line-who">
                {l.who === 'user' ? 'Кандидат' : l.who === 'ai' ? 'ИИ-интервьюер' : 'Система'}
              </div>
              <div className="line-text">{l.text}</div>
            </li>
          ))}
        </ol>
      </section>
    </main>
  );
}

function formatClock(total: number): string {
  const m = Math.floor(total / 60);
  const s = total % 60;
  return `${m}:${s.toString().padStart(2, '0')}`;
}
