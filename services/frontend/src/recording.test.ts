/**
 * Режим записи (FR-S8, ADR-009) — чистая логика: склейка транскрипта,
 * критерий «недостаточно речи», длительность «мм:сс».
 */
import { describe, expect, it } from 'vitest';
import {
  MIN_SPEECH_MS,
  formatRecDuration,
  isInsufficientSpeech,
  mergeRecording,
} from './recording';

describe('mergeRecording (склейка сегментов + интерим)', () => {
  it('склеивает сегменты и интерим пробелом, нормализует пробелы', () => {
    expect(mergeRecording(['Привет, меня', 'зовут Артём'], 'расскажите о себе')).toBe(
      'Привет, меня зовут Артём расскажите о себе',
    );
    expect(mergeRecording(['  Раз  ', 'два'], '   ')).toBe('Раз два');
  });

  it('пусто: нет сегментов и интерима', () => {
    expect(mergeRecording([], '')).toBe('');
  });
});

describe('isInsufficientSpeech («недостаточно речи»)', () => {
  it('пустой текст — недостаточно (даже при большом speech_ms)', () => {
    expect(isInsufficientSpeech('', 9999)).toBe(true);
  });

  it('речь < MIN_SPEECH_MS (1.5 с) — недостаточно', () => {
    expect(isInsufficientSpeech('М', MIN_SPEECH_MS - 1)).toBe(true);
    expect(isInsufficientSpeech('М', 0)).toBe(true);
  });

  it('речь ≥ MIN_SPEECH_MS — достаточно', () => {
    expect(isInsufficientSpeech('Достаточная реплика', MIN_SPEECH_MS)).toBe(false);
    expect(isInsufficientSpeech('Достаточная реплика', 90_000)).toBe(false);
  });
});

describe('formatRecDuration («мм:сс»)', () => {
  const now = 1_000_000;
  it('старт null → 00:00', () => {
    expect(formatRecDuration(null, now)).toBe('00:00');
  });
  it('45 с → 00:45; 65 с → 01:05; 905 с → 15:05', () => {
    expect(formatRecDuration(now - 45_000, now)).toBe('00:45');
    expect(formatRecDuration(now - 65_000, now)).toBe('01:05');
    expect(formatRecDuration(now - 905_000, now)).toBe('15:05');
  });
});
