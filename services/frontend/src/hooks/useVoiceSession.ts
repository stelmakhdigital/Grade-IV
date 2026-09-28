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
  type Session,
  type SessionEvent,
} from '../api';
import { MicCapture, type MicState } from '../audio/mic';
import { PcmPlayer } from '../audio/player';
import { SessionWS, type StageTask, type WsMessage } from '../ws';
import { apiErrorMessage } from '../views/LoginView';
import { eventKindLabel, eventText } from '../labels';

export interface Line {
  who: 'user' | 'ai' | 'system';
  text: string;
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
  const [speaking, setSpeaking] = useState(false);
  const [wsState, setWsState] = useState<'connecting' | 'open' | 'closed' | 'error'>('connecting');
  const [error, setError] = useState<string | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);

  const wsRef = useRef<SessionWS | null>(null);
  const micRef = useRef<MicCapture | null>(null);
  const micLevelRef = useRef(0); // RMS с worklet (эквалайзер)
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
      micLevelRef.current = 0;
      setMic('stopped');
      return;
    }
    try {
      // Клик — user-gesture: разрешаем браузеру вернуть аудио-контекст
      // плеера в running (иначе TTS может молчать, а флаг «ИИ говорит»
      // висеть — см. player.resume()).
      playerRef.current?.resume();
      await micCap.start({
        onChunk: (pcm) => {
          // Эхо-подавление (бэклог AEC, упрощение MVP): пока ИИ говорит
          // (плеер воспроизводит TTS-буфер) — микрофон не шлём (иначе
          // динамик → микрофон → VAD «речь кандидата»).
          if (playerRef.current?.isSpeaking()) return;
          wsRef.current?.sendPcm(pcm);
        },
        onLevel: (rms) => {
          micLevelRef.current = rms;
        },
        onState: (s) => setMic(s),
        onError: (msg) => setError(msg),
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
    speaking,
    wsState,
    error,
    micLevelRef,
    live,
    toggleMic,
    onStageAction,
    onFinish,
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
