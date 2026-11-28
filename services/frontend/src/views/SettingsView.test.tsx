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
const SETTINGS = {
  app_env: 'dev', applies: 'restart', applies_note: 'x', writable: true,
  paths: { app_file: 'config/app.json', voice_file: 'config/voice-models.json' },
  llm: {
    base_url: { value: 'http://llm:8000/v1', source: 'env', env: 'LLM_BASE_URL' },
    model: { value: 'Qwen3.8', source: 'file', env: 'LLM_MODEL' },
    enable_thinking: { value: false, source: 'default', env: 'LLM_ENABLE_THINKING' },
    mock: false, api_key_set: true, config_path: 'config/app.json',
  },
  voice: {
    address: { value: 'http://127.0.0.1:8100', source: 'default', env: 'VOICE_URL' },
    current: {
      stt: { provider: 'gigaam', model: 'v3_e2e_rnnt', loaded: true },
      tts: { provider: 'silero', model: 'v5_ru', loaded: true, speakers: ['xenia', 'dmitri'] },
    },
    catalog: {
      stt: { gigaam: [
        { name: 'v3_e2e_rnnt', present: true, missing: [] },
        { name: 'v3_ctc', present: false, missing: ['/models/v3_ctc.ckpt'] },
      ] },
      tts: { silero: [{ name: 'v5_ru', present: true, missing: [] }] },
    },
  },
  sandbox: { url: { value: 'http://localhost:8200', source: 'env', env: 'SANDBOX_URL' }, admin_token_set: true, tasks_refill_s: 600, tasks_min: 4 },
  timing: {
    silence_nudge_s: { value: 30, source: 'env', env: 'SILENCE_NUDGE_S' },
    vad_end_silence_ms: { value: 900, source: 'default', env: 'VAD_END_SILENCE_MS' },
    vad_rms_threshold: { value: 100, source: 'default', env: 'VAD_RMS_THRESHOLD' },
    vad_prestt_silence_ms: { value: 400, source: 'default', env: 'VAD_PRESTT_SILENCE_MS' },
    session_pause_timeout_s: { value: 1800, source: 'default', env: 'SESSION_PAUSE_TIMEOUT_S' },
    session_limit_s: { value: 0, source: 'default', env: 'SESSION_LIMIT_S' },
  },
  install: { database_url: 'sqlite:///./data/grade.db', addr: ':8010', log_level: 'info',
    jwt_secret_set: true, metrics_password_set: false, editable: false,
    note: 'адрес БД, порты и секреты — переменные окружения.' },
  pending: [
    { key: 'llm.model', status: 'needs_restart', env: 'LLM_MODEL', note: 'н' },
    { key: 'timing.silence_nudge_s', status: 'shadowed_by_env', env: 'SILENCE_NUDGE_S', note: 'п' },
  ],
};

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
    'GET /settings': SETTINGS,
    'PUT /settings': (_u: string, init?: RequestInit) =>
      __api.json(200, { applied: { app: { saved: true, applies: 'restart' } }, applies: 'restart', pending: SETTINGS.pending }),
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

  it('стендовые секции видны, секретов в DOM нет', async () => {
    renderView(true);
    expect(await screen.findByRole('heading', { name: 'Песочница и установка' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Тайминги и пороги' })).toBeInTheDocument();
    const banner = await screen.findByRole('status');
    expect(banner.textContent).toContain('ждут перезапуска');
    expect(banner.textContent).toContain('timing.silence_nudge_s (SILENCE_NUDGE_S)');
    expect(screen.getByText('sqlite:///./data/grade.db')).toBeInTheDocument();
    expect(document.body.textContent).not.toMatch(/jwt_secret[^_]/);
  });

});
