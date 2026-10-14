/**
 * Голосовая сессия (WP-8) — вся логика вне UI: загрузка сессии (REST),
 * WebSocket (таймер, живой транскрипт, стадии, кадры PCM), микрофон
 * (AudioWorklet → PCM-кадры), TTS-плеер, уровни для эквалайзера.
 * SessionView — только рендер + обработчики на возвращённом API.
 */
import { useCallback, useEffect, useRef, useState } from 'react';
import {
  getSession,
  listEvents,
  pauseSession,
  resumeSession,
  type Session,
  type SessionEvent,
} from '../api';
import { MicCapture, type MicDebugInfo, type MicState } from '../audio/mic';
import { PcmPlayer } from '../audio/player';
import { SessionWS, type StageTask, type WsMessage } from '../ws';
import {
  MIN_SPEECH_MS,
  isInsufficientSpeech,
  mergeRecording,
} from '../recording';
import { apiErrorMessage } from '../views/LoginView';
import { eventKindLabel, eventText } from '../labels';

export interface Line {
  who: 'user' | 'ai' | 'system';
  text: string;
  /** Интеримная (stt_partial) строка кандидата — обновляется до final. */
  interim?: boolean;
}

export function useVoiceSession(id: number) {
  const [session, setSession] = useState<Session | null>(null);
  const [lines, setLines] = useState<Line[]>([]);
  // Текущая стадия UI: из session.stage (REST) и WS stage-сообщений.
  const [stage, setStage] = useState<string>('voice');
  const [runReview, setRunReview] = useState<string>('');
  const [designReview, setDesignReview] = useState<string>('');
  const [task, setTask] = useState<StageTask | null>(null);
  // Задачи стадии Live-Code (spec-13B T2): tasks из stage-сообщения; task = tasks[0] (совместимость).
  const [tasks, setTasks] = useState<StageTask[]>([]);
  const [remainingS, setRemainingS] = useState<number | null>(null);
  const [lastAiText, setLastAiText] = useState<string>('');
  const [mic, setMic] = useState<MicState>('idle');
  const [micDbg, setMicDbg] = useState<MicDebugInfo | null>(null);
  const [speaking, setSpeaking] = useState(false);
  const [wsState, setWsState] = useState<'connecting' | 'open' | 'reconnecting' | 'closed' | 'error'>('connecting');
  const [error, setError] = useState<string | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  // Пауза (FR-S7): uiPaused — локальное состояние после клика; если страница
  // перезагружена на paused-сессии, статус приходит из REST (session.status).
  const [uiPaused, setUiPaused] = useState(false);
  const [pauseBusy, setPauseBusy] = useState(false);
  // Режим записи (FR-S8, ADR-009): микрофон включён — сервис слушает и
  // распознаёт налёт, но сегменты НЕ становятся ответами кандидата; склеенный
  // транскрипт отправляется ОДНИМ сообщением при выключении микрофона /
  // «Отправить сейчас» (реф — WS-колбэк создан один раз, state в замыкании
  // бы устаревал).
  const [recActive, setRecActive] = useState(false);
  const [recSegs, setRecSegs] = useState<string[]>([]);
  const [recPartial, setRecPartial] = useState('');
  const [recTotalSpeechMs, setRecTotalSpeechMs] = useState(0);
  const [recSince, setRecSince] = useState<number | null>(null); // мм:сс окна
  const [recCollapsed, setRecCollapsed] = useState(false);
  const [recNote, setRecNote] = useState<string | null>(null);
  const recRef = useRef<{ active: boolean }>({ active: false });

  const wsRef = useRef<SessionWS | null>(null);
  const micRef = useRef<MicCapture | null>(null);
  const micLevelRef = useRef(0); // RMS с worklet (эквалайзер)
  const playerRef = useRef<PcmPlayer | null>(null);
  const stageRef = useRef<string>('voice');
  // TTS-статус → мик: детектор «молчащего» микрофона не кормим, пока ИИ говорит
  // (TTS-эхо) и TTS_TAIL_GRACE_MS после (кандидат думает/слушает) — иначе ложная
  // алерт «Микрофон молчит» на обычной паузе диалога (инцидент 2026-10-09).
  useEffect(() => {
    micRef.current?.setTtsSpeaking(speaking);
  }, [speaking]);
  // Микрофон был включён до паузы — при «Продолжить» пытаемся вернуть захват.
  const micBeforePauseRef = useRef(false);

  // spec-14/D1: latest-value рефes для WS-колбэков (замыкание reconnect-цикла
  // живёт между рендерами — статус/пауза должны читаться актуальными).
  const statusRef = useRef<string>('');
  statusRef.current = session?.status ?? '';
  const uiPausedRef = useRef(false);
  uiPausedRef.current = uiPaused;

  const live = session !== null && (session.status === 'active' || session.status === 'paused');
  const paused = uiPaused || session?.status === 'paused';

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
    const micCap = new MicCapture();
    const player = new PcmPlayer();
    micRef.current = micCap;
    playerRef.current = player;
    setWsState('connecting');

    // spec-14/D1: авто-reconnect с backoff. Сервер при обрыве WS ставит
    // активную сессию в паузу (engine.Detach) — при переподключении
    // возобновляем по REST и ре-синким режим записи (BUG-20261012-018).
    let reconnectAttempts = 0;
    let reconnectTimer: number | null = null;
    let disposed = false;
    let resumeNeeded = false; // переподключение после неожиданного обрыва

    const onOpen = () => {
      setWsState('open');
      reconnectAttempts = 0;
      // Ре-синк режима записи (BUG-20261012-018): при переподключении WS
      // серверная сессия новая (recording=false), а клиент мог остаться в
      // режиме записи — без ре-синка нагнанное в это время не попало бы в
      // буфер, а final-ы улетали бы отдельными сообщениями.
      if (recRef.current.active) {
        wsRef.current?.sendUi('recording', { on: true });
      }
      if (resumeNeeded) {
        resumeNeeded = false;
        // Обрыв при активной сессии: сервер её спаузил — возобновляем по REST.
        resumeSession(id).catch(() => {
          void refreshStatus(); // 409 (pause_timeout/уже active) → статус-поллинг
        });
      }
    };
    const onMessage = (m: WsMessage) => {
        switch (m.type) {
          case 'stage':
            setStage(m.name);
            stageRef.current = m.name;
            {
              const list = m.tasks ?? (m.task ? [m.task] : []);
              setTasks(list);
              setTask(list[0] ?? null);
            }
            setRunReview('');
            setDesignReview('');
            setRemainingS(null);
            break;
          case 'timer':
            setRemainingS(m.remaining_s);
            break;
          case 'ai_text':
            setLastAiText(m.text);
            if (stageRef.current === 'livecode') {
              setRunReview(m.text);
            } else if (stageRef.current === 'design') {
              setDesignReview(m.text);
            }
            break;
          case 'transcript':
            setLines((prev) => {
              const next = { who: (m.who === 'user' ? 'user' : 'ai') as Line['who'], text: m.text };
              // final кандидата фиксирует интерим-строку (stt_partial),
              // а не дублирует её отдельной строкой.
              const last = prev[prev.length - 1];
              if (m.who === 'user' && last !== undefined && last.who === 'user' && last.interim) {
                return [...prev.slice(0, -1), next];
              }
              return [...prev.slice(-199), next];
            });
            break;
          case 'stt_partial':
            // Стриминговый STT (ADR-007): интеримный текст кандидата.
            // Режим записи (FR-S8): интерим — в окно записи, НЕ в строки
            // (сегменты не становятся ответами кандидата).
            if (recRef.current.active) {
              setRecPartial(m.text);
              break;
            }
            // обновляем последнюю интерим-строку (или создаём её).
            setLines((prev) => {
              const last = prev[prev.length - 1];
              if (last !== undefined && last.who === 'user' && last.interim) {
                return [...prev.slice(0, -1), { ...last, text: m.text }];
              }
              return [...prev.slice(-199), { who: 'user', text: m.text, interim: true }];
            });
            break;
          case 'stt_segment':
            // Режим записи (FR-S8, ADR-009): распознанный сегмент (final по
            // тишине) — в накопитель окна записи; AI-ход не стартует (сервер
            // подавляет turn, пока recording on). Вне записи — игнор
            // (backward-compat: старые серверы событие не шлют).
            if (!recRef.current.active) {
              break;
            }
            setRecSegs((prev) => [...prev, m.text]);
            setRecPartial('');
            setRecTotalSpeechMs(m.total_speech_ms ?? 0);
            break;
          case 'tts_stop':
            // barge-in: кандидат прервал речь ИИ — плеер останавливается
            // немедленно (включая запланированные кадры), очередь сбрасывается.
            player.stop();
            setSpeaking(false);
            break;
          case 'error':
            // spec-14/D2: серверные коды завершения сессии (wsSendError):
            // session_ended приходит при reconnect на закрытую сессию и при
            // pause/finish на чужой — раньше клиент ждал только session_aborted
            // и «Сессия прервана» не срабатывала по WS.
            if (m.code === 'session_aborted' || m.code === 'session_ended' || m.code === 'not_found') {
              setError(m.code === 'session_ended' ? 'Сессия уже завершена.' : 'Сессия прервана.');
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
      };
    const onAudio = (pcm: Int16Array, _isLast: boolean) => {
      player.playChunk(pcm);
      setSpeaking(true);
    };
    const onClose = (reason: 'closed' | 'error' | 'aborted') => {
      setSpeaking(false);
      if (reason === 'aborted') {
        // spec-14/D2: 1011/1002 — сервер завершил сессию (finish/session_ended).
        setWsState('closed');
        setError('Сессия завершена сервером.');
        void refreshStatus();
        return;
      }
      // spec-14/D1: неожиданный обрыв при активной (не спаузенной нами)
      // сессии — авто-reconnect с backoff (иначе микрофон «говорил в пустоту»,
      // а визуализатор дышал от локального RMS).
      if (statusRef.current === 'active' && !uiPausedRef.current) {
        resumeNeeded = true;
        reconnect();
        return;
      }
      setWsState(reason === 'error' ? 'error' : 'closed');
    };

    function wire() {
      const ws = new SessionWS();
      wsRef.current = ws;
      ws.connect(id, { onOpen, onMessage, onAudio, onClose });
    }
    function reconnect() {
      if (disposed) return;
      reconnectAttempts += 1;
      if (reconnectAttempts > 5) {
        // 5 попыток исчерпаны — честно: сервер спаузил сессию, «Продолжить»
        // или перезагрузка вернут WS.
        setWsState('closed');
        setError('Соединение потеряно. Обновите страницу — сессия на паузе.');
        return;
      }
      setWsState('reconnecting');
      const delay = Math.min(16000, 1000 * 2 ** (reconnectAttempts - 1));
      reconnectTimer = window.setTimeout(() => {
        if (!disposed) wire();
      }, delay);
    }

    wire();

    const speakingTimer = window.setInterval(() => {
      setSpeaking(player.isSpeaking());
    }, 300);
    // Живая диагностика микрофона (dev): снапшот захвата на экран
    // (только пока захват активен — после паузы/выключения не мигаем).
    const dbgTimer = window.setInterval(() => {
      const st = micCap.state;
      setMicDbg(st === 'running' || st === 'muted' ? micCap.debugInfo() : null);
    }, 1500);

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
      disposed = true;
      if (reconnectTimer !== null) window.clearTimeout(reconnectTimer);
      window.clearInterval(speakingTimer);
      window.clearInterval(dbgTimer);
      micCap.dispose(); // полное освобождение (tracks/контекст) при unmount
      player.dispose();
      wsRef.current?.close();
      wsRef.current = null;
      micRef.current = null;
      playerRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session === null ? 'none' : session.status, id, pushLine]);

  // Старт захвата микрофона (общий путь: кнопка «Включить микрофон» и
  // автоматический возврат после «Продолжить» — оба из user-gesture).
  const startMic = async () => {
    const micCap = micRef.current;
    if (micCap === null) return;
    // Клик — user-gesture: разрешаем браузеру вернуть аудио-контекст
    // плеера в running (иначе TTS может молчать, а флаг «ИИ говорит»
    // висеть — см. player.resume()).
    playerRef.current?.resume();
    await micCap.start({
      onChunk: (pcm) => {
        // PCM шлём всегда (и во время речи ИИ): серверный VAD ведёт
        // barge-in (SRS §8) — короткое «эхо»/всплеск отбрасывается,
        // завершённая реплика ≥ 500 мс прерывает TTS. Эхо-подавление —
        // echoCancellation в getUserMedia (mic.ts).
        wsRef.current?.sendPcm(pcm);
      },
      onLevel: (rms) => {
        micLevelRef.current = rms;
      },
      onState: (s) => setMic(s),
      onError: (msg) => setError(msg),
      // info-сообщения (fallback-захват и т.п.) — в строку статуса/ошибки.
      onInfo: (msg) => setError(msg),
    });
  };

    // Конец записи (FR-S8): recording off + склеенный транскрипт ОДНИМ
  // utterance-сообщением (WS-порядок: off раньше utterance) → AI-ход стартует.
  // «Недостаточно речи» (< MIN_SPEECH_MS или пусто) — не отправляем.
  // spec-14/D3: результаты sendUi проверяются — при закрытом WS наговоренный
  // текст больше не теряется молча: буфер сохраняется, повторная отправка —
  // «Отправить» (кнопка микрофона) после восстановления соединения.
  const sendBufferedRecording = useCallback(() => {
    const merged = mergeRecording(recSegs, recPartial);
    const insufficient = isInsufficientSpeech(merged, recTotalSpeechMs);
    const ws = wsRef.current;
    if (ws === null || !ws.sendUi('recording', { on: false })) {
      setRecNote('Нет соединения: наговоренный текст сохранён — восстановите соединение и нажмите «Отправить» повторно.');
      return;
    }
    if (insufficient) {
      setRecSegs([]);
      setRecPartial('');
      setRecTotalSpeechMs(0);
      setRecNote(`Недостаточно речи для отправки (минимум ~${Math.round(MIN_SPEECH_MS / 100) / 10} с).`);
      return;
    }
    if (!ws.sendUi('utterance', { text: merged })) {
      // WS закрылся между off и utterance — буфер цел, текст не потерян.
      setRecNote('Соединение потеряно во время отправки — нажмите «Отправить» повторно.');
      return;
    }
    setRecSegs([]);
    setRecPartial('');
    setRecTotalSpeechMs(0);
    setRecNote(null);
  }, [recSegs, recPartial, recTotalSpeechMs]);

  const finishRecording = useCallback(() => {
    if (!recRef.current.active) return;
    recRef.current.active = false;
    setRecActive(false);
    setRecSince(null);
    setRecNote(null);
    sendBufferedRecording();
  }, [sendBufferedRecording]);

  const toggleMic = async () => {
    const micCap = micRef.current;
    if (micCap === null) return;
    if (mic === 'running' || mic === 'muted') {
      micCap.stop(); // soft: mute без разрыва потока (FR-S8)
      micLevelRef.current = 0;
      setMic('stopped');
      setMicDbg(null);
      finishRecording(); // выключить микрофон — отправить склеенный транскрипт
      return;
    }
    try {
      await startMic();
      // Режим записи (FR-S8): включил микрофон — сервис слушает и склеивает
      // транскрипт; ответ кандидата — только при выключении/«Отправить сейчас».
      // BUG-20261012-018: если WS закрыт, сервер не узнает о записи и кадры не
      // уйдут — честно сообщаем и не включаем «режим записи» (иначе кандидат
      // диктует в пустоту: «наговариваю, а не записывается в буфер»).
      const recStarted = wsRef.current?.sendUi('recording', { on: true }) ?? false;
      if (!recStarted) {
        micCap.stop();
        micLevelRef.current = 0;
        setMic('stopped');
        setMicDbg(null);
        setError('Нет соединения с сессией — включите микрофон после восстановления.');
        return;
      }
      recRef.current.active = true;
      setRecActive(true);
      setRecSegs([]);
      setRecPartial('');
      setRecTotalSpeechMs(0);
      setRecSince(Date.now());
      setRecNote(null);
      setRecCollapsed(false);
    } catch {
      setMic('denied');
      setError('Нет доступа к микрофону — разрешите в браузере.');
    }
  };

  // «Очистить буфер» (окно записи): сброс накопленного транскрипта, запись
  // продолжается — кандидат может надиктовать ответ заново (решение 2026-10-09).
  // Отправка — только кнопкой «Отправить» (эквивалент выключения микрофона).
  const clearRecording = useCallback(() => {
    if (!recRef.current.active) return;
    setRecSegs([]);
    setRecPartial('');
    setRecTotalSpeechMs(0);
    setRecNote(null);
  }, []);

  // Пауза сессии (FR-S7): тарификация останавливается на сервере.
  // Локально: микрофон (остановка отправки PCM), TTS-плеер (stop),
  // paused-состояние UI. session.status НЕ меняем — смена статуса
  // перезапустила бы WS-эффект (переподключение), а WS на паузе живёт.
  const pause = async () => {
    if (session === null || session.status !== 'active' || paused || pauseBusy) return;
    setPauseBusy(true);
    try {
      micBeforePauseRef.current = mic === 'running' || mic === 'muted';
      // Пауза — полное освобождение микрофона (долго), не мягкий мьут;
      // незавершённая запись сбрасывается (ADR-009: запись не переживает паузу).
      if (recRef.current.active) {
        recRef.current.active = false;
        wsRef.current?.sendUi('recording', { on: false });
        setRecActive(false);
        setRecSince(null);
        setRecSegs([]);
        setRecPartial('');
        setRecTotalSpeechMs(0);
      }
      micRef.current?.dispose();
      micLevelRef.current = 0;
      setMic('stopped');
      setMicDbg(null);
      playerRef.current?.stop();
      setSpeaking(false);
      await pauseSession(id);
      setUiPaused(true);
    } catch (e) {
      setError(apiErrorMessage(e));
    } finally {
      setPauseBusy(false);
    }
  };

  // Возобновление (FR-S7): сервер снимает паузу; UI — в active. Если
  // микрофон был включён до паузы — возвращаем захват (клик — user-gesture,
  // autoplay OK); ошибка старта не ломает сессию — сообщение + кнопка.
  const resume = async () => {
    if (session === null || !paused || pauseBusy) return;
    setPauseBusy(true);
    try {
      try {
        playerRef.current?.resume();
      } catch {
        // Аудио-контекст может быть недоступен — не критично: плеер
        // восстановит контекст на первом TTS-кадре (в jsdom нет AudioContext).
      }
      await resumeSession(id);
      setUiPaused(false);
      if (micBeforePauseRef.current) {
        micBeforePauseRef.current = false;
        try {
          await startMic();
        } catch {
          // onError из MicCapture уже показал причину — просто не вешаем
          // флаг «включён», кандидат нажмёт «Включить микрофон» сам.
          setMic('stopped');
        }
      }
    } catch (e) {
      // M2: resume упал (409 «недопустимый переход ... active → active») — вероятно,
      // сессия УЖЕ активна (десинк/reconnect: локальный paused-флаг true, а сервер
      // active — double-resume/pause не применилась). Ре-синк: если active — это
      // no-op («продолжить» = уже продолжается), без страшной 409-ошибки. Явный
      // REST-ответ resume-active остаётся 409 (test-verified); здесь просто не
      // показываем её, когда сессия фактически активна.
      try {
        const s2 = await getSession(id);
        setSession(s2);
        if (s2.status === 'active') {
          setUiPaused(false);
          return;
        }
        if (s2.status === 'aborted') {
          // spec-14/D12 (BUG-20261014-021): пауза превысила порог — resume сам
          // прервал сессию. Честно и без сырой 409-ошибки: отчёт ниже.
          setUiPaused(false);
          setError('Пауза превысила порог (30 мин) — интервью автоматически завершено. Ниже — отчёт.');
          try {
            const ev = await listEvents(id);
            setLines(ev.map((e) => lineFromEvent(e)));
          } catch {
            // транскрипт не критичен — отчёт/события догрузит перезагрузка
          }
          return;
        }
      } catch {
        // ре-синк не критичен — покажем исходную ошибку ниже
      }
      setError(apiErrorMessage(e));
    } finally {
      setPauseBusy(false);
    }
  };

  const onStageAction = (stage: string) => {
    wsRef.current?.sendUi('stage_action', { stage });
  };

  const onFinish = () => {
    wsRef.current?.sendUi('finish');
  };

  return {
    session,
    loadError,
    lines,
    stage,
    runReview,
    designReview,
    task,
    tasks,
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
    // Режим записи (FR-S8, ADR-009).
    recActive,
    recSegs,
    recPartial,
    recTotalSpeechMs,
    recSince,
    recCollapsed,
    setRecCollapsed,
    recNote,
    finishRecording,
    sendBufferedRecording,
    clearRecording,
  };
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
