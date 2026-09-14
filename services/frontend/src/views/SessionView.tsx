/**
 * Страница сессии (WP-8): голосовое интервью.
 * Активна/пауза — WebSocket: таймер, живой транскрипт, микрофон (AudioWorklet
 * → PCM-кадры), TTS-воспроизведение, стадии, finish.
 * Завершена/прекращена — только чтение (история событий из /events).
 */
import { useCallback, useEffect, useRef, useState } from 'react';
import {
  getSession,
  listEvents,
  type Session,
  type SessionEvent,
} from '../api';
import { useAuth } from '../auth';
import { MicCapture, type MicState } from '../audio/mic';
import { PcmPlayer } from '../audio/player';
import { SessionWS, type StageTask, type WsMessage } from '../ws';
import { apiErrorMessage } from './LoginView';
import { eventKindLabel, eventText, statusLabel, stageLabel } from '../labels';
import { LiveCodePanel } from './livecode/LiveCodePanel';

interface Line {
  who: 'user' | 'ai' | 'system';
  text: string;
}

export function SessionView({ id }: { id: number }) {
  const { user, logout } = useAuth();
  const [session, setSession] = useState<Session | null>(null);
  const [lines, setLines] = useState<Line[]>([]);
  // Текущая стадия UI: из session.stage (REST) и WS stage-сообщений.
  const [stage, setStage] = useState<string>('voice');
  const [runReview, setRunReview] = useState<string>('');
  const [task, setTask] = useState<StageTask | null>(null);
  const [remainingS, setRemainingS] = useState<number | null>(null);
  const [lastAiText, setLastAiText] = useState<string>('');
  const [mic, setMic] = useState<MicState>('idle');
  const [speaking, setSpeaking] = useState(false);
  const [wsState, setWsState] = useState<'connecting' | 'open' | 'closed' | 'error'>('connecting');
  const [error, setError] = useState<string | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);

  const wsRef = useRef<SessionWS | null>(null);
  const micRef = useRef<MicCapture | null>(null);
  const playerRef = useRef<PcmPlayer | null>(null);
  const stageRef = useRef<string>('voice');

  const live = session !== null && (session.status === 'active' || session.status === 'paused');

  // Загрузка метаданных (+ история для завершённых).
  useEffect(() => {
    let alive = true;
    (async () => {
      try {
        const s = await getSession(id);
        if (!alive) return;
        setSession(s);
        stageRef.current = s.stage;
        setStage(s.stage);
        if (s.status === 'finished' || s.status === 'aborted') {
          const ev = await listEvents(id);
          if (!alive) return;
          setLines(ev.map((e) => lineFromEvent(e)));
        }
      } catch (e) {
        if (alive) setLoadError(apiErrorMessage(e));
      }
    })();
    return () => {
      alive = false;
    };
  }, [id]);

  const pushLine = useCallback((who: Line['who'], text: string) => {
    setLines((prev) => [...prev.slice(-199), { who, text }]);
  }, []);

  // WS-цикл (только для активных сессий).
  useEffect(() => {
    if (session === null || !live) return;
    const ws = new SessionWS();
    const micCap = new MicCapture();
    const player = new PcmPlayer();
    wsRef.current = ws;
    micRef.current = micCap;
    playerRef.current = player;
    setWsState('connecting');

    ws.connect(id, {
      onOpen: () => setWsState('open'),
      onMessage: (m: WsMessage) => {
        switch (m.type) {
          case 'stage':
            setStage(m.name);
            stageRef.current = m.name;
            setTask(m.task ?? null);
            setRunReview('');
            setRemainingS(null);
            break;
          case 'timer':
            setRemainingS(m.remaining_s);
            break;
          case 'ai_text':
            setLastAiText(m.text);
            if (stageRef.current === 'livecode') {
              setRunReview(m.text);
            }
            break;
          case 'transcript':
            setLines((prev) => [
              ...prev.slice(-199),
              { who: m.who === 'user' ? 'user' : 'ai', text: m.text },
            ]);
            break;
          case 'error':
            if (m.code === 'session_aborted') {
              setError('Сессия прервана.');
              void refreshStatus();
            } else {
              setError(m.msg);
            }
            break;
          case 'run_result':
            pushLine('system', runResultLine(m));
            setRunReview('');
            break;
          case 'report_ready':
            pushLine('system', 'Отчёт готов.');
            void refreshStatus();
            break;
        }
      },
      onAudio: (pcm, _isLast) => {
        player.playChunk(pcm);
        setSpeaking(true);
      },
      onClose: (reason) => {
        setWsState(reason === 'error' ? 'error' : 'closed');
        setSpeaking(false);
        if (reason === 'aborted') {
          setError('Сессия прервана.');
          void refreshStatus();
        }
      },
    });

    const speakingTimer = window.setInterval(() => {
      setSpeaking(player.isSpeaking());
    }, 300);

    async function refreshStatus() {
      try {
        const s = await getSession(id);
        setSession(s);
        if (s.status === 'finished' || s.status === 'aborted') {
          const ev = await listEvents(id);
          setLines(ev.map((e) => lineFromEvent(e)));
          setWsState('closed');
        }
      } catch {
        // статус-опрос не критичен
      }
    }

    return () => {
      window.clearInterval(speakingTimer);
      micCap.stop();
      player.dispose();
      ws.close();
      wsRef.current = null;
      micRef.current = null;
      playerRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session === null ? 'none' : session.status, id, pushLine]);

  const toggleMic = async () => {
    const micCap = micRef.current;
    if (micCap === null) return;
    if (mic === 'running') {
      micCap.stop();
      setMic('stopped');
      return;
    }
    try {
      await micCap.start({
        onChunk: (pcm) => {
          wsRef.current?.sendPcm(pcm);
        },
        onState: (s) => setMic(s),
      });
    } catch {
      setMic('denied');
      setError('Нет доступа к микрофону — разрешите в браузере.');
    }
  };

  const onStageAction = (stage: string) => {
    wsRef.current?.sendUi('stage_action', { stage });
  };

  const onFinish = () => {
    wsRef.current?.sendUi('finish');
  };

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
            {speaking && <span className="saying">ИИ говорит…</span>}
            {mic === 'running' && <span className="listening">микрофон: включён</span>}
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
            {mic === 'denied' && (
              <span className="form-error">Нет доступа к микрофону.</span>
            )}
            <button
              type="button"
              className={mic === 'running' ? 'btn danger' : 'btn primary'}
              onClick={() => void toggleMic()}
              data-testid="mic-toggle"
              disabled={wsState !== 'open'}
            >
              {mic === 'running' ? 'Выключить микрофон' : 'Включить микрофон'}
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
        <section className="card notice">
          <p>
            Интервью {statusLabel(session.status)}. Ниже — запись диалога.
          </p>
        </section>
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

function lineFromEvent(e: SessionEvent): Line {
  const kind = e.kind;
  const who: Line['who'] =
    kind === 'user_utterance' ? 'user'
    : kind === 'ai_utterance' || kind === 'ai_nudge' ? 'ai'
    : 'system';
  const text = eventText(kind, e.data);
  if (text !== '') return { who, text };
  // события без текста — короткая системная строка
  return { who: 'system', text: eventKindLabel(kind) };
}

function runResultLine(m: { [k: string]: unknown }): string {
  const passed = m.passed === true ? 'успех' : m.passed === false ? 'неудача' : '';
  return passed !== '' ? `Тесты: ${passed}` : 'Результат выполнения кода получен.';
}
