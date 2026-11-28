/**
 * Страница сессии (WP-8): голосовое интервью.
 * Чистый UI: вся логика (WS, микрофон, плеер, таймер, транскрипт) — в
 * useVoiceSession. Рендер: активная сессия (голос/Live-Code/Design),
 * завершённая (запись диалога + отчёт).
 */
import { Suspense, lazy, useEffect, useRef, useState } from 'react';
import { useAuth } from '../auth';
import { MicVisualizer, type MicEqMode } from './MicVisualizer';
import { useVoiceSession, MAX_TRANSCRIPT_LINES } from '../hooks/useVoiceSession';
import { num } from '../format';
import { micDebugEnabled } from '../audio/mic';
import { formatRecDuration, mergeRecording, MIN_SPEECH_MS } from '../recording';
import { formatClock, practiceLabel, statusLabel, stageLabel } from '../labels';
import { PanelLoading } from './PanelLoading';

// Тяжёлые панели — лениво (B2): чанки Live-Code (Monaco-обёртка), System
// Design (Excalidraw) и отчёт грузятся по факту открытия соответствующей
// вкладки/стадии, а не вместе с first-load кабинета. Пока ленивый чанк
// загружается, Suspense показывает skeleton (PanelLoading).
const LiveCodePanel = lazy(() =>
  import('./livecode/LiveCodePanel').then((m) => ({ default: m.LiveCodePanel })),
);
const DesignPanel = lazy(() =>
  import('./design/DesignPanel').then((m) => ({ default: m.DesignPanel })),
);
const ReportView = lazy(() =>
  import('./report/ReportView').then((m) => ({ default: m.ReportView })),
);

export function SessionView({ id }: { id: number }) {
  const { user } = useAuth();
  const {
    session,
    loadError,
    lines,
    stage,
    runReview,
    designReview,
    board,
    trackMeta,
    task,
    tasks,
    remainingS,
    lastAiText,
    mic,
    micDbg,
    speaking,
    wsState,
    error,
    stageDebug,
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
    recTotalSpeechMs,
    clearRecording,
    sendTextUtterance,
  } = useVoiceSession(id);

  // Окно записи (FR-S8): «мм:сс» — тик каждую секунду, пока запись активна.
  const [recNow, setRecNow] = useState(() => Date.now());
  useEffect(() => {
    if (!recActive) return;
    const t = window.setInterval(() => setRecNow(Date.now()), 1000);
    return () => window.clearInterval(t);
  }, [recActive]);
  const recText = mergeRecording(recSegs, recPartial);
  const waitingForAI = live && !paused && speaking && mic !== 'running' && mic !== 'muted';

  // Текстовый ввод реплик (spec-18 #5, fallback микрофона): отправка — кнопкой
  // или Enter через существующий канал 'utterance'. Очищаем поле только при
  // УСПЕШНОЙ отправке — при обрыве WS текст остаётся (повторная отправка,
  // как у D3 с наговоренным буфером). Ход занят (waitingForAI) — кнопка
  // заблокирована; повторная реплика при занятом ходе всё равно
  // отбрасывается busy-гейтом сервера.
  const [typedAnswer, setTypedAnswer] = useState('');
  const submitTypedAnswer = () => {
    if (typedAnswer.trim() === '') return;
    if (sendTextUtterance(typedAnswer)) setTypedAnswer('');
  };

  // spec-14/D14: завершение — необратимое (стоп интервью + генерация отчёта)
  // — подтверждаем двойным кликом (окно 5 с), без window.confirm.
  const [confirmFinish, setConfirmFinish] = useState(false);
  useEffect(() => {
    if (!confirmFinish) return;
    const t = window.setTimeout(() => setConfirmFinish(false), 5000);
    return () => window.clearTimeout(t);
  }, [confirmFinish]);

  // Если интервьюер отвечает дольше 30 с — кандидату нужно сказать, что
  // делать, а не показывать вечное «Ожидание ИИ…» (ui-review 2026-11-27).
  const [aiStalled, setAiStalled] = useState(false);
  useEffect(() => {
    setAiStalled(false);
    if (!waitingForAI) return;
    const t = window.setTimeout(() => setAiStalled(true), 30_000);
    return () => window.clearTimeout(t);
  }, [waitingForAI]);

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

  // Заголовок — должность из резюме, а не название роли: кандидат на Python,
  // приведший к роли backend, не должен сидеть в «Интервью: Backend-разработчик».
  const interviewTitle = session.resume_title?.trim() || trackMeta?.title || session.grade;
  // Язык практики — язык сессии: роль умеет несколько, а по резюме выбран один
  // (у роли по умолчанию может стоять go, а кандидат пишет python).
  const practiceRuntime = session.stack || trackMeta?.practice?.runtime;
  // Языки, заявленные в резюме кроме кодового: по ним ставим вопросы, код — по stack.
  const otherStacks = (session.resume_stacks ?? []).filter((s) => s !== session.stack);

  return (
    <main className="app session-page">
      <header className="header session-head">
        <div>
          <h1>
            {/* spec-20: имя роли понятно, но должностью из резюме не пренебрегаем;
                стек — только у кодовых ролей. */}
            Интервью: {interviewTitle}
            {session.stack ? ` / ${session.stack}` : ''}
          </h1>
          <p className="muted small">
            {user?.email} · <span className={`status ${paused ? 'paused' : session.status}`}>{statusLabel(paused ? 'paused' : session.status)}</span>
            {live && (
              <span>
                {' · '}
                {stage === 'livecode' && practiceLabel(practiceRuntime) !== null
                  ? `Практика (${practiceLabel(practiceRuntime)})`
                  : stageLabel(stage)}
              </span>
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
            <button
              type="button"
              className="btn ghost small"
              title="Завершение остановит интервью и запустит генерацию отчёта"
              onClick={() => {
                if (confirmFinish) {
                  setConfirmFinish(false);
                  onFinish();
                } else {
                  setConfirmFinish(true);
                }
              }}
              data-testid={confirmFinish ? 'finish-confirm' : 'finish'}
            >
              {confirmFinish ? 'Точно завершить? (клик — подтвердить)' : 'Завершить интервью'}
            </button>
          )}
          <a className="btn ghost small" href="#/">В кабинет</a>
        </div>
      </header>

      {/* Рейка стадий (дизайн 2026-11-25): программа роли, текущая стадия,
          остаток лимита и параметры интервью — то, к чему возвращаются
          посреди работы. Раньше это было размазано по подзаголовку
          и строке статуса голосовой панели. */}
      <aside className="session-rail" aria-label="Программа интервью">
        <ol className="ladder" data-testid="stage-ladder">
          {(() => {
            const program = (trackMeta?.stages ?? ['voice', 'livecode', 'design']).filter((s) => s !== 'report');
            const at = program.indexOf(stage);
            return program.map((s, i) => (
              <li
                key={s}
                className={`ladder-step${s === stage ? ' current' : at >= 0 && i < at ? ' done' : ''}`}
              >
                <span className="ladder-idx" aria-hidden="true">{i + 1}</span>
                <span className="ladder-name">{stageLabel(s)}</span>
                {s === stage && <span className="sr-only"> — текущая стадия</span>}
              </li>
            ));
          })()}
        </ol>

        {/* Лимит времени сессии: сервер шлёт timer/remaining_s. Цифра — в рейке
            (крупно, моно); на узких экранах рейка становится горизонтальной
            полосой, элемент тот же. */}
        {live && remainingS !== null && remainingS > 0 && (
          <p
            className={`rail-time${remainingS < 300 ? ' warn' : ''}`}
            role="timer"
            aria-label={`Осталось ${formatClock(remainingS)}`}
            data-testid="session-timer"
          >
            <span className="rail-time-label">до конца</span>
            <span className="rail-time-value">{formatClock(remainingS)}</span>
          </p>
        )}

        <dl className="rail-meta">
          <dt>Роль</dt>
          <dd>{interviewTitle}</dd>
          {session.stack && (
            <>
              <dt>Стек</dt>
              <dd>{session.stack}</dd>
            </>
          )}
          {otherStacks.length > 0 && (
            <>
              <dt>Ещё языки</dt>
              <dd>{otherStacks.join(', ')}</dd>
            </>
          )}
          {session.base_grade && (
            <>
              {/* «Цель» показывала базу по резюме: в резюме middle, интервью на
                  senior, а в рельсе читалось «цель middle». */}
              <dt>База по резюме</dt>
              <dd>{session.base_grade}</dd>
            </>
          )}
          <dt>Цель</dt>
          <dd>{session.grade}</dd>
        </dl>
      </aside>

      {/* Всё, что происходит справа от рейки, — одной колонкой: баннер паузы,
          пульт, отчёт, диалог. См. .session-work в index.css. */}
      <div className="session-work">

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
                : wsState === 'reconnecting' ? 'переподключение…'
                : wsState === 'error' ? 'ошибка соединения'
                : 'соединение закрыто'}
            </span>
            {(wsState === 'closed' || wsState === 'error') && (
              <button
                type="button"
                className="btn ghost small"
                onClick={() => window.location.reload()}
                data-testid="ws-reconnect"
              >
                Переподключиться
              </button>
            )}
            {speaking && !recActive && <span className="saying">ИИ говорит — чтобы ответить, дождитесь паузы</span>}
            {mic === 'running' && <span className="listening">микрофон: включён</span>}
            {mic === 'muted' && (
              <span className="form-error" role="alert" data-testid="mic-muted">
                Микрофон молчит — проверьте устройство, мьют и разрешения браузера.{' '}
                <a className="debug-link" href="/audio-debug.html" target="_blank" rel="noreferrer" data-testid="debug-link-mic-muted">/audio-debug.html</a>{' '}
                — диагностика микрофона.
              </span>
            )}
            {micDebugEnabled() && micDbg !== null && micDbg.chunks > 0 && (
              <span className="mic-dbg" title="Диагностика захвата: путь, частота чанков, уровень">
                мик[{micDbg.path}]: чанков {micDbg.chunks} · шаг {micDbg.medGapMs} мс · max rms {micDbg.maxRms} · ctx {micDbg.ctxState}
              </span>
            )}
          </div>

          {/* Отладка стадий (dev: ENABLE_DEBUG=1 на api). Переключатель даёт
              любую стадию программы роли — не нужно каждый раз прогонять
              интервью с начала, чтобы попасть на доску или практику. */}
          {stageDebug && (
            <div
              className="stage-debug"
              role="group"
              aria-label="Отладка стадий"
              data-testid="stage-debug"
            >
              <span className="muted small">отладка: перейти к</span>
              {(trackMeta?.stages ?? ['voice', 'livecode', 'design'])
                .filter((s) => s !== 'report' && s !== stage)
                .map((s) => (
                  <button
                    key={s}
                    type="button"
                    className="btn ghost small"
                    onClick={() => onStageAction(s)}
                    data-testid={`debug-stage-${s}`}
                  >
                    {stageLabel(s)}
                  </button>
                ))}
            </div>
          )}

          {/* spec-21 (CoderPad: честное предупреждение о десктопе): на узких
              экранах практика/доска тесны — подсказка видна только на мобиле. */}
          {(stage === 'livecode' || stage === 'design') && (
            <p className="touch-hint muted small" data-testid="touch-hint">
              Эта стадия удобнее с клавиатуры — на телефоне может быть тесно.
            </p>
          )}

          {stage === 'livecode' && session !== null ? (
            <>
              <Suspense fallback={<PanelLoading />}>
                {/* spec-19: пустой stack сессии на livecode-стадии — shell-трек (сисадмин). */}
                <LiveCodePanel
                  sessionId={id}
                  stack={session.stack || 'shell'}
                  tasks={tasks}
                  taskId={task?.id}
                  taskTitle={task?.title}
                  taskFiles={task?.files ?? null}
                  review={runReview}
                  paused={paused}
                />
              </Suspense>
              {/* Кнопка перехода в System Design: по программе стадий трека
                  (trackMeta.stages, spec-20) — design стоит ПОСЛЕ livecode.
                  BUG-20261104-026: раньше правило было по грейду (junior —
                  livecode→report), и junior на shell-треке (design есть на всех
                  грейдах) застревал на livecode без перехода. Легаси-сессии без
                  track.stages — прежнее грейд-правило. */}
              {(() => {
                const st = trackMeta?.stages;
                const nextDesign = st
                  ? st.includes('design') && st.includes('livecode') && st.indexOf('design') > st.indexOf('livecode')
                  : session.grade !== 'junior';
                return (
                  nextDesign && (
                    <div className="stage-nav" data-testid="stage-nav">
                      <button
                        type="button"
                        className="btn ghost"
                        onClick={() => onStageAction('design')}
                        data-testid="go-design"
                      >
                        К System Design
                      </button>
                    </div>
                  )
                );
              })()}
            </>
          ) : stage === 'design' ? (
            <>
              <Suspense fallback={<PanelLoading />}>
                <DesignPanel sessionId={id} review={designReview} board={board} paused={paused} />
              </Suspense>
              {/* Возврат на предыдущую стадию разрешён (движок); холст при возврате
                  восстанавливается из сохранённого whiteboard. */}
              <div className="stage-nav" data-testid="stage-nav">
                {(trackMeta == null || trackMeta.practice != null) && (
                  <button
                    type="button"
                    className="btn ghost"
                    onClick={() => onStageAction('livecode')}
                    data-testid="go-livecode-back"
                  >
                    К Live-Code (назад)
                  </button>
                )}
              </div>
            </>
          ) : (
            <>
          {task !== null && task.statement !== undefined && (
            <div className="task-box">
              <strong>{task.title ?? task.id}</strong>
              <p>{task.statement}</p>
            </div>
          )}

          {lastAiText !== '' && (
            // Реплики интервьюера приходят во время работы над задачей: без
            // aria-live скринридер о них не узнает, а для голосового
            // интервью это основной контент.
            <p className="ai-say" data-testid="ai-say" role="status" aria-live="polite">
              {lastAiText}
            </p>
          )}

          {/* Окно записи (FR-S8, ADR-009): склеивающийся транскрипт +
              длительность + статус; сворачивается; «Очистить буфер» — сброс
              накопленного, запись продолжается (ответ можно надиктовать заново).
              Отправка — кнопкой «Отправить» (эквивалент выключения микрофона). */}
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
                  {/* spec-14/D15: живой прогресс минимума речи — раньше
                      «недостаточно речи» выяснялось постфактум в recNote. */}
                  <span className="rec-speech-hint" data-testid="rec-speech-hint">
                    речи {num(recTotalSpeechMs / 1000, 1)} с · нужно ≥ {num(MIN_SPEECH_MS / 1000, 1)} с
                  </span>
                  <button
                    type="button"
                    className="btn ghost small"
                    onClick={() => clearRecording()}
                    disabled={recText === '' && recTotalSpeechMs === 0}
                    data-testid="rec-clear"
                  >
                    Очистить буфер
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
              className={
                // На паузе primary только одна — «Продолжить» в шапке:
                // две синие кнопки рядом спорят за главное действие.
                paused
                  ? 'btn ghost'
                  : mic === 'running' || mic === 'muted' || !waitingForAI
                    ? 'btn primary'
                    : 'btn ghost'
              }
              onClick={() => void toggleMic()}
              data-testid="mic-toggle"
              disabled={wsState !== 'open' || paused || waitingForAI}
              title={waitingForAI ? 'Дождитесь ответа ИИ-интервьюера' : undefined}
            >
              {mic === 'running' || mic === 'muted'
                ? 'Отправить'
                : waitingForAI
                  ? 'Ожидание ИИ…'
                  : 'Включить микрофон'}
            </button>
            {/* Текстовый ввод реплик (spec-18 #5): fallback микрофона —
                шумная среда / нет доступа к устройству. Канал — тот же
                'utterance', что и у записи. Скрыт на паузе (реплики не
                обрабатываются, FR-S7) и не на голосовой стадии. */}
            {!paused && (
              <span className="text-answer-box">
                <input
                  type="text"
                  className="text-answer"
                  placeholder="Ответ текстом"
                  value={typedAnswer}
                  onChange={(e) => setTypedAnswer(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                      e.preventDefault();
                      submitTypedAnswer();
                    }
                  }}
                  aria-label="Ответ текстом"
                  data-testid="text-answer"
                />
                <button
                  type="button"
                  className="btn ghost"
                  onClick={submitTypedAnswer}
                  disabled={
                    waitingForAI || wsState !== 'open' || typedAnswer.trim() === ''
                  }
                  title={waitingForAI ? 'Дождитесь ответа ИИ-интервьюера' : undefined}
                  data-testid="text-answer-send"
                >
                  Отправить
                </button>
              </span>
            )}
            {/* Легаси-сессии без меты трека: прежняя кнопка. Новые роли —
                «К практике (…)» выше. */}
            {stage === 'voice' && trackMeta == null && (
              <button type="button" className="btn ghost" onClick={() => onStageAction('livecode')}>
                К Live-Code
              </button>
            )}
            {/* spec-22: роль с практикой — из голосовой на практику (язык сессии). */}
            {stage === 'voice' && trackMeta != null && trackMeta.practice != null && (
              <button type="button" className="btn ghost" onClick={() => onStageAction('livecode')} data-testid="go-livecode-voice">
                К практике ({practiceLabel(practiceRuntime)})
              </button>
            )}
            {/* Роль без практики — из голосовой сразу на доску. */}
            {stage === 'voice' && trackMeta != null && trackMeta.practice == null && (
              <button type="button" className="btn ghost" onClick={() => onStageAction('design')} data-testid="go-design-voice">
                К доске
              </button>
            )}
            {/* Переход livecode → design — в панели livecode (go-design): там
                видно программу стадий трека, и здесь кнопка дублировалась. */}
          </div>

          {/* Причина, почему микрофон и отправка текстом сейчас неактивны:
              на disabled-кнопке её не объявить, поэтому она текстом рядом.
              Скринридеру — polite, чтобы не перекрикивать реплику ИИ. */}
          {waitingForAI && (
            <p className="muted small" role="status" data-testid="wait-hint">
              {aiStalled
                ? 'Интервьюер отвечает дольше обычного. Можно подождать или завершить интервью кнопкой «Завершить интервью».'
                : 'Интервьюер формулирует вопрос — отвечать можно, когда он закончит.'}
            </p>
          )}
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
        <Suspense fallback={<PanelLoading />}>
          <ReportView sessionId={id} shell={trackMeta?.practice?.runtime === 'shell'} />
        </Suspense>
        </>
      )}

      <section className="card transcript" aria-label="Транскрипт">
        {lines.length === 0 && <p className="muted">Диалог появится здесь.</p>}
        {/* Кап держим во вьюшке, но молчать о нём — выглядит как потеря записи. */}
        {lines.length >= MAX_TRANSCRIPT_LINES && (
          <p className="muted small" data-testid="transcript-cap">
            Показаны последние {MAX_TRANSCRIPT_LINES} реплик. Полная запись — в
            отчёте после завершения.
          </p>
        )}
        <ol className="transcript-list" data-testid="lines" ref={transcriptRef}>
          {[...lines].reverse().map((l) => (
            <li key={l.id} className={`line ${l.who}${l.interim ? ' interim' : ''}`}>
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
      </div>
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
