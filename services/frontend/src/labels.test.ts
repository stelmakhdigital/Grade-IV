import { describe, expect, it } from 'vitest';
import { eventKindLabel, eventText, statusLabel, stageLabel } from './labels';

describe('подписи (labels)', () => {
  it('стадии и статусы', () => {
    expect(stageLabel('voice')).toBe('Голос');
    expect(stageLabel('livecode')).toBe('Live-Code');
    expect(stageLabel('design')).toBe('System Design');
    expect(stageLabel('report')).toBe('Отчёт');
    expect(statusLabel('active')).toBe('активна');
    expect(statusLabel('finished')).toBe('завершена');
    expect(statusLabel('unknown')).toBe('unknown');
  });

  it('события: тексты', () => {
    expect(eventKindLabel('user_utterance')).toBe('Кандидат');
    expect(eventKindLabel('ai_utterance')).toBe('ИИ-интервьюер');
    expect(eventText('user_utterance', { text: 'привет' })).toBe('привет');
    expect(eventText('user_utterance', {})).toBe('');
    expect(eventText('stage_change', { from: 'voice', to: 'livecode' })).toBe('стадия: Live-Code');
    expect(eventText('code_run', { passed: true, duration_ms: 12 })).toBe('тесты: успех');
    expect(eventText('session_created', { grade: 'middle', stack: 'go' })).toBe('middle / go');
    expect(eventText('paused', {})).toBe('');
  });
});
