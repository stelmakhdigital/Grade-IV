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
  const [remainingS, setRemainingS] = useState<number | null>(null);
  const [lastAiText, setLastAiText] = useState<string>('');
  const [mic, setMic] = useState<MicState>('idle');
  const [micDbg, setMicDbg] = useState<MicDebugInfo | null>(null);
  const [speaking, setSpeaking] = useState(false);
  const [wsState, setWsState] = useState<'connecting' | 'open' | 'closed' | 'error'>('connecting');
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
  // Микрофон был включён до паузы — при «Продолжить» пытаемся вернуть захват.
  const micBeforePauseRef = useRef(false);

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
      window.clearInterval(speakingTimer);
      window.clearInterval(dbgTimer);
      micCap.dispose(); // полное освобождение (tracks/контекст) при unmount
      player.dispose();
      ws.close();
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
  const finishRecording = useCallback(() => {
    if (!recRef.current.active) return;
    recRef.current.active = false;
    setRecActive(false);
    setRecSince(null);
    const merged = mergeRecording(recSegs, recPartial);
    const insufficient = isInsufficientSpeech(merged, recTotalSpeechMs);
    setRecSegs([]);
    setRecPartial('');
    setRecTotalSpeechMs(0);
    wsRef.current?.sendUi('recording', { on: false });
    if (insufficient) {
      setRecNote(`Недостаточно речи для отправки (минимум ~${Math.round(MIN_SPEECH_MS / 100) / 10} с).`);
      return;
    }
    setRecNote(null);
    wsRef.current?.sendUi('utterance', { text: merged });
  }, [recSegs, recPartial, recTotalSpeechMs]);

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
      wsRef.current?.sendUi('recording', { on: true });
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
