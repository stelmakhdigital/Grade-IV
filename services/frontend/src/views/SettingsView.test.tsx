/**
 * Экран настроек (#/settings): тема применяется сразу, голос и характер
 * сохраняются как выбор по умолчанию, блок «Стенд» — только админу.
 */
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { setToken } from '../api';
import { AuthProvider } from '../auth';
import { prefProfileId, prefSpeaker } from '../prefs';
import { SettingsView } from './SettingsView';

const VOICES = {
  default: 'xenia',
  voices: [
    { id: 'v1', name: 'Есения', gender: 'female', speaker: 'xenia' },
    { id: 'v2', name: 'Дмитрий', gender: 'male', speaker: 'dmitri' },
  ],
};
const PROFILES = [
  { id: 1, name: 'Наставник', tone: 'supportive', difficulty: 'standard', description: '' },
  { id: 2, name: 'Строгий', tone: 'demanding', difficulty: 'hard', description: '' },
];
const RUNTIME = {
  app_env: 'dev',
  applies: 'restart',
  applies_note: 'x',
  llm: {
    base_url: { value: 'http://llm:8000/v1', source: 'env', env: 'LLM_BASE_URL' },
    model: { value: 'Qwen3.8', source: 'default', env: 'LLM_MODEL' },
    enable_thinking: { value: false, source: 'default', env: 'LLM_ENABLE_THINKING' },
    mock: false,
    api_key_set: true,
    config_path: 'config/llm.json',
  },
  voice: {
    current: {
      stt: { provider: 'gigaam', model: 'v3_e2e_rnnt', loaded: true },
      tts: { provider: 'silero', model: 'v5_ru', loaded: true, speakers: ['xenia'] },
    },
  },
  sandbox: { url: 'http://localhost:8200', admin_token_set: true, tasks_refill_s: 600, tasks_min: 4 },
};

function me(isAdmin: boolean) {
  return { user: { id: 1, email: 'a@example.com', created_at: '2026-01-01T00:00:00Z', is_admin: isAdmin } };
}

function renderView(isAdmin = true) {
  __api.on({
    '/auth/me': me(isAdmin),
    '/tts-voices': VOICES,
    '/profiles': PROFILES,
    '/admin/runtime': RUNTIME,
  });
  return render(
    <AuthProvider>
      <SettingsView />
    </AuthProvider>,
  );
}

beforeEach(() => {
  localStorage.clear();
  setToken('tok');
  document.documentElement.removeAttribute('data-theme');
  // Системная тема в jsdom: matchMedia нет, а тема на нём держится.
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches: query.includes('light') && false,
    addEventListener: () => {},
    removeEventListener: () => {},
  }));
});

describe('Экран настроек', () => {
  it('тема применяется сразу и подписывает, кто её задал', async () => {
    renderView(false);
    const note = await screen.findByText(/Сейчас тёмная/);
    expect(note.textContent).toContain('как в системе');

    await userEvent.click(screen.getByRole('button', { name: 'Светлая' }));
    expect(document.documentElement.dataset.theme).toBe('light');
    expect(localStorage.getItem('grade-theme')).toBe('light');
    await waitFor(() => expect(screen.getByText(/Сейчас светлая/).textContent).toContain('выбрано вручную'));
  });

  it('голос и характер сохраняются как выбор по умолчанию', async () => {
    renderView(false);
    const speaker = await screen.findByLabelText('Голос интервьюера');
    expect(speaker).toHaveValue('xenia');

    await userEvent.selectOptions(speaker, 'dmitri');
    expect(prefSpeaker()).toBe('dmitri');

    await userEvent.selectOptions(screen.getByLabelText('Профиль интервьюера'), '2');
    expect(prefProfileId()).toBe(2);
  });

  it('сохранённый выбор, которого нет в списках, не подставляется', async () => {
    localStorage.setItem('grade.tts-speaker', 'удалённый-админом');
    renderView(false);
    expect(await screen.findByLabelText('Голос интервьюера')).toHaveValue('xenia');
  });

  it('блок «Стенд» видит только админ', async () => {
    renderView(false);
    await screen.findByLabelText('Голос интервьюера');
    expect(screen.queryByRole('heading', { name: 'Стенд' })).not.toBeInTheDocument();

    renderView(true);
    expect(await screen.findByRole('heading', { name: 'Стенд' })).toBeInTheDocument();
    expect(screen.getByText('Qwen3.8')).toBeInTheDocument();
    expect(screen.getByText('gigaam · v3_e2e_rnnt')).toBeInTheDocument();
    expect(screen.getByText('http://localhost:8200')).toBeInTheDocument();
    expect(screen.getByText(/ключи —\s*только переменные окружения/i)).toBeInTheDocument();
  });

  it('недоступный стенд не ломает раздел', async () => {
    __api.on({
      '/auth/me': me(true),
      '/tts-voices': VOICES,
      '/profiles': PROFILES,
      '/admin/runtime': () => __api.failure(503, 'voice_unavailable', 'voice не отвечает'),
    });
    render(<AuthProvider><SettingsView /></AuthProvider>);
    expect(await screen.findByText(/Сводка недоступна/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Светлая' })).toBeEnabled();
  });
});
