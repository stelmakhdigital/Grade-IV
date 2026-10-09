/**
 * Страница сессии (WP-8): голосовое интервью.
 * Чистый UI: вся логика (WS, микрофон, плеер, таймер, транскрипт) — в
 * useVoiceSession. Рендер: активная сессия (голос/Live-Code/Design),
 * завершённая (запись диалога + отчёт).
 */
import { useEffect, useRef, useState } from 'react';
import { useAuth } from '../auth';
import { MicVisualizer, type MicEqMode } from './MicVisualizer';
import { useVoiceSession } from '../hooks/useVoiceSession';
import { micDebugEnabled } from '../audio/mic';
import { formatRecDuration, mergeRecording } from '../recording';
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
    micDbg,
    speaking,
    wsState,
    error,
    micLevelRef,
    live,
    paused,
    pauseBusy,
    toggleMic,
    pause,
    resume,
    onStageAction,
    onFinish,
    recActive,
    recSegs,
    recPartial,
    recSince,
    recCollapsed,
    setRecCollapsed,
    recNote,
    sendRecordingNow,
  } = useVoiceSession(id);

  // Окно записи (FR-S8): «мм:сс» — тик каждую секунду, пока запись активна.
  const [recNow, setRecNow] = useState(() => Date.now());
  useEffect(() => {
    if (!recActive) return;
    const t = window.setInterval(() => setRecNow(Date.now()), 1000);
    return () => window.clearInterval(t);
  }, [recActive]);
  const recText = mergeRecording(recSegs, recPartial);

  // История диалога — новые СВЕРХУ (FR-S8): lines хранятся как есть (append,
  // кап 200), рендер — перевёрнутый порядок (interim — последняя — наверху).
  // Автоскролл: при новом сообщении/обновлении interim — контейнер к началу
  // (наверх), если пользователь у верхнего края (userNearTop, < 80 px);
  // прокрутил вниз в старые — не трогаем.
  const transcriptRef = useRef<HTMLOListElement>(null);
  useEffect(() => {
    const el = transcriptRef.current;
    if (el === null || el.scrollTop >= 80) return;
    if (typeof el.scrollTo === 'function') {
      el.scrollTo({ top: 0 });
    } else {
      el.scrollTop = 0; // jsdom: scrollTo отсутствует
    }
  }, [lines]);

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
            {user?.email} · <span className={`status ${paused ? 'paused' : session.status}`}>{statusLabel(paused ? 'paused' : session.status)}</span>
            {live && remainingS !== null && (
              <span data-testid="timer"> · осталось {formatClock(remainingS)}</span>
            )}
            {live && (
              <span> · {stageLabel(stage)}</span>
            )}
          </p>
        </div>
        <div className="head-actions">
          {live && !paused && (
            <button
              type="button"
              className="btn ghost small"
              onClick={() => void pause()}
              data-testid="pause-btn"
              disabled={pauseBusy}
            >
              Пауза
            </button>
          )}
          {live && paused && (
            <button
              type="button"
              className="btn primary small"
              onClick={() => void resume()}
              data-testid="resume-btn"
              disabled={pauseBusy}
            >
              Продолжить
            </button>
          )}
          {live && (
            <button type="button" className="btn ghost small" onClick={onFinish}>
              Завершить интервью
            </button>
          )}
          <a className="btn ghost small" href="#/">В кабинет</a>
        </div>
      </header>

      {live && paused && (
        <div className="card notice" role="status" data-testid="paused-banner">
          <strong>Сессия на паузе</strong> — время не тарифицируется.
          Нажмите «Продолжить», чтобы вернуться к интервью.
        </div>
      )}

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
                Микрофон молчит — проверьте устройство, мьют и разрешения браузера.{' '}
                <a className="debug-link" href="/audio-debug.html" target="_blank" rel="noreferrer" data-testid="debug-link-mic-muted">/audio-debug.html</a>{' '}
                — диагностика микрофона.
              </span>
            )}
            {mic !== 'running' && mic !== 'denied' && mic !== 'muted' && stage === 'voice' && wsState === 'open' && !paused && (
              <span className="form-error" role="alert">
                Микрофон выключен — ИИ вас не слышит. Нажмите «Включить микрофон».
              </span>
            )}
            {micDebugEnabled() && micDbg !== null && micDbg.chunks > 0 && (
              <span className="mic-dbg" title="Диагностика захвата: путь, частота чанков, уровень">
                мик[{micDbg.path}]: чанков {micDbg.chunks} · шаг {micDbg.medGapMs} мс · max rms {micDbg.maxRms} · ctx {micDbg.ctxState}
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

          {/* Окно записи (FR-S8, ADR-009): склеивающийся транскрипт +
              длительность + статус; сворачивается; «Отправить сейчас» —
              досрочная отправка (микрофон по умолчанию выключается). */}
          {recActive && (
            <div className="rec-window" data-testid="rec-window">
              <div className="rec-head">
                <span className="rec-status" data-testid="rec-status">
                  ⏺ Запись {formatRecDuration(recSince, recNow)}
                </span>
                <button
                  type="button"
                  className="btn ghost small"
                  onClick={() => setRecCollapsed(!recCollapsed)}
                  data-testid="rec-collapse"
                >
                  {recCollapsed ? 'Развернуть' : 'Свернуть'}
                </button>
              </div>
              {!recCollapsed && (
                <>
                  <p className="rec-text" data-testid="rec-text">
                    {recText !== '' ? recText : 'Говорите — транскрипт появится здесь…'}
                  </p>
                  <button
                    type="button"
                    className="btn primary small"
                    onClick={() => void sendRecordingNow()}
                    data-testid="rec-send"
                  >
                    Отправить сейчас
                  </button>
                </>
              )}
            </div>
          )}
          {recNote !== null && !recActive && (
            <p className="form-error" role="status" data-testid="rec-note">
              {recNote}
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
              disabled={wsState !== 'open' || paused}
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
            <p className="form-error" role="alert">{renderDebugLink(error)}</p>
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
        <ol className="transcript-list" data-testid="lines" ref={transcriptRef}>
          {[...lines].reverse().map((l, i) => (
            <li key={i} className={`line ${l.who}${l.interim ? ' interim' : ''}`}>
              <div className="line-who">
                {l.who === 'user' ? 'Кандидат' : l.who === 'ai' ? 'ИИ-интервьюер' : 'Система'}
              </div>
              <div className="line-text">{l.text}</div>
              {l.interim && (
                <span className="interim-dots" aria-hidden="true">распознаётся ⋯</span>
              )}
            </li>
          ))}
        </ol>
      </section>
    </main>
  );
}

/**
 * Рендер строки ошибки: путь /audio-debug.html — кликабельная ссылка
 * (самодиагностика микрофона; дефект (b) инцидента 2026-10-08).
 */
function renderDebugLink(msg: string) {
  const marker = '/audio-debug.html';
  const idx = msg.indexOf(marker);
  if (idx === -1) return msg;
  return (
    <>
      {msg.slice(0, idx)}
      <a className="debug-link" href={marker} target="_blank" rel="noreferrer" data-testid="debug-link">{marker}</a>
      {msg.slice(idx + marker.length)}
    </>
  );
}

function formatClock(total: number): string {
  const m = Math.floor(total / 60);
  const s = total % 60;
  return `${m}:${s.toString().padStart(2, '0')}`;
}
