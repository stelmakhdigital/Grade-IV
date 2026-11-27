/**
 * Строки транскрипта голосовой сессии: тип Line, их нумерация, маппинг
 * событий REST и WS-сообщений в строки и чистые обновления массива строк.
 * Состояние (useState) держит хук useVoiceSession — он здесь единственный потребитель.
 */
import type { SessionEvent } from '../api';
import { eventKindLabel, eventText } from '../labels';

export interface Line {
  who: 'user' | 'ai' | 'system';
  text: string;
  /** Интеримная (stt_partial) строка кандидата — обновляется до final. */
  interim?: boolean;
  /** spec-15: стабильный ключ для списка транскрипта (ключ по обратному
   *  индексу вызывал churn ре-рендеров при новых строках). */
  id: number;
}

let lineSeq = 0;
export const nextLineId = () => ++lineSeq;

/** Сколько последних реплик держим во вьюшке (ui-review 2026-11-27): на
 * длинном интервью старые уходят, и об этом надо сказать вслух. */
export const MAX_TRANSCRIPT_LINES = 200;

// Финал кандидата заменяет собой интерим-строку (stt_partial),
// где бы она ни стояла. Между partial и final успевает встать
// реплика интервьюера (подсказка за молчание), и ориентироваться
// на «последнюю строку» нельзя: интерим оставался не зафиксированным
// («распознаётся ⋯» горело на уже готовой фразе) и рядом
// вырастала вторая строка с тем же текстом.
export function applyFinalTranscript(prev: Line[], who: 'user' | 'ai', text: string): Line[] {
  let idx = -1;
  if (who === 'user') {
    for (let i = prev.length - 1; i >= 0; i--) {
      const l = prev[i];
      if (l !== undefined && l.who === 'user' && l.interim) {
        idx = i;
        break;
      }
    }
  }
  if (idx >= 0) {
    const old = prev[idx]!;
    const next: Line = {
      who: 'user',
      // Пустой финал не должен стирать распознанное: оставляем
      // текст интерима, но снимаем признак (точки гаснут).
      text: text !== '' ? text : old.text,
      id: old.id,
    };
    const copy = prev.slice();
    copy[idx] = next;
    return copy;
  }
  const next: Line = {
    who: (who === 'user' ? 'user' : 'ai') as Line['who'],
    text: text,
    id: nextLineId(),
  };
  return [...prev.slice(-(MAX_TRANSCRIPT_LINES - 1)), next];
}

// обновляем последнюю интерим-строку (или создаём её).
export function applySttPartial(prev: Line[], text: string): Line[] {
  const last = prev[prev.length - 1];
  if (last !== undefined && last.who === 'user' && last.interim) {
    return [...prev.slice(0, -1), { ...last, text: text }];
  }
  return [...prev.slice(-(MAX_TRANSCRIPT_LINES - 1)), { who: 'user', text: text, interim: true, id: nextLineId() }];
}

export function lineFromEvent(e: SessionEvent): Line {
  const kind = e.kind;
  const who: Line['who'] =
    kind === 'user_utterance' ? 'user'
    : kind === 'ai_utterance' || kind === 'ai_nudge' ? 'ai'
    : 'system';
  const text = eventText(kind, e.data);
  if (text !== '') return { who, text, id: nextLineId() };
  // события без текста — короткая системная строка
  return { who: 'system', text: eventKindLabel(kind), id: nextLineId() };
}

export function runResultLine(m: { [k: string]: unknown }): string {
  const passed = m.passed === true ? 'успех' : m.passed === false ? 'неудача' : '';
  return passed !== '' ? `Тесты: ${passed}` : 'Результат выполнения кода получен.';
}
