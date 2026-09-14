/** Подписи стадий/статусов/событий (русский, SRS). */
import type { SessionStatus, Stage } from './api';

export function stageLabel(stage: Stage | string): string {
  switch (stage) {
    case 'voice':
      return 'Голос';
    case 'livecode':
      return 'Live-Code';
    case 'design':
      return 'System Design';
    case 'report':
      return 'Отчёт';
    default:
      return stage;
  }
}

export function statusLabel(status: SessionStatus | string): string {
  switch (status) {
    case 'active':
      return 'активна';
    case 'paused':
      return 'пауза';
    case 'finished':
      return 'завершена';
    case 'aborted':
      return 'прекращена';
    default:
      return status;
  }
}

/** Человекочитаемые подписи событий транскрипта (session_events.kind). */
export function eventKindLabel(kind: string): string {
  switch (kind) {
    case 'session_created':
      return 'Сессия создана';
    case 'stage_change':
      return 'Смена стадии';
    case 'user_utterance':
      return 'Кандидат';
    case 'ai_utterance':
      return 'ИИ-интервьюер';
    case 'ai_nudge':
      return 'ИИ (подсказка)';
    case 'code_run':
      return 'Выполнение кода';
    case 'whiteboard_save':
      return 'Сохранение схемы';
    case 'paused':
      return 'Пауза';
    case 'resumed':
      return 'Возобновление';
    case 'finished':
      return 'Завершение';
    case 'aborted':
      return 'Прекращение';
    default:
      return kind;
  }
}

/** Текст события для отображения в транскрипте (data — произвольный JSON). */
export function eventText(kind: string, data: Record<string, unknown>): string {
  const d = data as Record<string, unknown>;
  switch (kind) {
    case 'user_utterance':
    case 'ai_utterance':
    case 'ai_nudge':
      return typeof d.text === 'string' ? d.text : '';
    case 'stage_change':
      return typeof d.to === 'string' ? `стадия: ${stageLabel(d.to)}` : '';
    case 'code_run': {
      const passed = typeof d.passed === 'boolean' ? (d.passed ? 'успех' : 'неудача') : '';
      return passed ? `тесты: ${passed}` : '';
    }
    case 'session_created': {
      const grade = typeof d.grade === 'string' ? d.grade : '';
      const stack = typeof d.stack === 'string' ? d.stack : '';
      return grade ? `${grade} / ${stack}` : '';
    }
    default:
      return '';
  }
}
