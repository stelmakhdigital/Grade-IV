/**
 * Текстовый ввод реплик на голосовой стадии (spec-18 #5): fallback микрофона
 * (шумная среда / нет доступа к устройству). Отправка через СУЩЕСТВУЮЩИЙ
 * канал 'utterance' (sendTextUtterance хука); поле очищается только при
 * успешной отправке; скрыт на паузе (FR-S7).
 *
 * Отдельный файл со своим моком useVoiceSession — существующий
 * SessionView.test.tsx не трогаем (его fakeVoiceSession не знает про
 * sendTextUtterance).
 */
import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createElement } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { SessionView } from './SessionView';

// Мок хука целиком в vi.hoisted (фабрика vi.mock поднята выше объявлений
// модуля — внешние переменные из модуля ей недоступны).
const t = vi.hoisted(() => {
  const sendTextUtterance = vi.fn<(text: string) => boolean>(() => true);
  const state = { paused: false, speaking: false, mic: 'idle', wsState: 'open' };
  const S_ACTIVE = {
    id: 9,
    grade: 'middle',
    stack: 'go',
    stage: 'voice',
    status: 'active',
  };
  const fakeSession = () => ({
    session: S_ACTIVE,
    loadError: null,
    lines: [],
    stage: 'voice',
    runReview: '',
    designReview: '',
    task: null,
    tasks: [],
    lastAiText: '',
    mic: state.mic,
    micDbg: null,
    speaking: state.speaking,
    wsState: state.wsState,
    error: null,
    micLevelRef: { current: 0 },
    live: true,
    paused: state.paused,
    pauseBusy: false,
    toggleMic: vi.fn(),
    pause: vi.fn(),
    resume: vi.fn(),
    onStageAction: vi.fn(),
    onFinish: vi.fn(),
    recActive: false,
    recSegs: [] as string[],
    recPartial: '',
    recSince: null,
    recCollapsed: false,
    setRecCollapsed: vi.fn(),
    recNote: null,
    recTotalSpeechMs: 0,
    finishRecording: vi.fn(),
    sendBufferedRecording: vi.fn(),
    clearRecording: vi.fn(),
    sendTextUtterance,
  });
  return { sendTextUtterance, state, fakeSession };
});

vi.mock('../hooks/useVoiceSession', () => ({
  useVoiceSession: () => t.fakeSession(),
}));

vi.mock('../auth', () => ({
  useAuth: () => ({ user: { email: 'cand@example.com' } }),
}));

function renderView() {
  return render(createElement(SessionView, { id: 9 }));
}

afterEach(() => {
  cleanup();
  t.sendTextUtterance.mockClear();
  t.sendTextUtterance.mockReturnValue(true);
  t.state.paused = false;
  t.state.speaking = false;
  t.state.mic = 'idle';
  t.state.wsState = 'open';
});

describe('текстовый ввод реплик (voice-стадия, spec-18 #5)', () => {
  it('инпут с плейсхолдером «Ответ текстом (необязательно)» рядом с микрофоном', () => {
    renderView();
    const input = screen.getByPlaceholderText('Ответ текстом (необязательно)');
    expect(input).toBeTruthy();
    expect(screen.getByTestId('mic-toggle')).toBeTruthy();
  });

  it('кнопка «Отправить» шлёт текст через sendTextUtterance и очищает поле', async () => {
    const user = userEvent.setup();
    renderView();
    const input = screen.getByPlaceholderText('Ответ текстом (необязательно)');
    await user.type(input, 'распределённый кеш через Redis');
    await user.click(screen.getByTestId('text-answer-send'));
    expect(t.sendTextUtterance).toHaveBeenCalledTimes(1);
    expect(t.sendTextUtterance).toHaveBeenCalledWith('распределённый кеш через Redis');
    expect((input as HTMLInputElement).value).toBe('');
  });

  it('Enter тоже отправляет', async () => {
    const user = userEvent.setup();
    renderView();
    const input = screen.getByPlaceholderText('Ответ текстом (необязательно)');
    await user.type(input, 'ответ{Enter}');
    expect(t.sendTextUtterance).toHaveBeenCalledTimes(1);
    expect(t.sendTextUtterance).toHaveBeenCalledWith('ответ');
  });

  it('кнопка заблокирована при пустом поле и когда ИИ отвечает (waitingForAI)', () => {
    const { unmount } = renderView();
    const send = screen.getByTestId('text-answer-send') as HTMLButtonElement;
    expect(send.disabled).toBe(true);
    unmount();
    cleanup();
    // speaking=true и микрофон не идёт → waitingForAI → disabled.
    t.state.speaking = true;
    renderView();
    const send2 = screen.getByTestId('text-answer-send') as HTMLButtonElement;
    expect(send2.disabled).toBe(true);
  });

  it('при обрыве WS (sendTextUtterance=false) текст НЕ очищается — повторная отправка', async () => {
    t.sendTextUtterance.mockReturnValue(false);
    const user = userEvent.setup();
    renderView();
    const input = screen.getByPlaceholderText('Ответ текстом (необязательно)');
    await user.type(input, 'черкнуть');
    await user.click(screen.getByTestId('text-answer-send'));
    expect(t.sendTextUtterance).toHaveBeenCalledWith('черкнуть');
    expect((input as HTMLInputElement).value).toBe('черкнуть');
  });

  it('на паузе инпут скрыт (реплики не обрабатываются, FR-S7)', () => {
    t.state.paused = true;
    renderView();
    expect(screen.queryByTestId('text-answer')).toBeNull();
  });
});
