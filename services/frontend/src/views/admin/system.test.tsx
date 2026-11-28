/**
 * Вкладка «Система» (src/views/admin/system.tsx): что подключено, откуда взялось
 * значение и что можно выбрать (фидбек 2026-11-28).
 */
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it } from 'vitest';
import { setToken } from '../../api';
import { AuthProvider } from '../../auth';
import { AdminView } from '../AdminView';

const ME_ADMIN = {
  user: { id: 1, email: 'admin@example.com', created_at: '2026-09-14T00:00:00Z', is_admin: true },
};

const RUNTIME = {
  app_env: 'dev',
  applies: 'restart',
  applies_note: 'модель загружается при старте — после перезапуска',
  llm: {
    base_url: { value: 'http://llm:8000/v1', source: 'file', env: 'LLM_BASE_URL' },
    model: { value: 'Qwen3.8-Flash', source: 'env', env: 'LLM_MODEL' },
    enable_thinking: { value: false, source: 'default', env: 'LLM_ENABLE_THINKING' },
    mock: false,
    api_key_set: true,
    config_path: 'config/llm.json',
  },
  voice: {
    path: 'config/voice-models.json',
    writable: true,
    applies: 'restart',
    current: {
      stt: { provider: 'gigaam', model: 'v3_e2e_rnnt', device: 'cpu', loaded: true },
      tts: { provider: 'silero', model: 'v5_ru', loaded: true, speakers: ['xenia', 'dmitri'] },
    },
    sources: {
      stt: {
        provider: { value: 'gigaam', source: 'default', env: 'VOICE_STT_PROVIDER' },
        model: { gigaam: { value: 'v3_e2e_rnnt', source: 'default', env: 'GIGAAM_MODEL' } },
      },
      tts: {
        provider: { value: 'silero', source: 'default', env: 'VOICE_TTS_PROVIDER' },
        speaker: { value: 'xenia', source: 'env', env: 'TTS_SPEAKER' },
      },
    },
    catalog: {
      stt: {
        gigaam: [
          { name: 'v3_e2e_rnnt', present: true, missing: [] },
          { name: 'v3_ctc', present: false, missing: ['/models/gigaam/v3_ctc.ckpt'] },
        ],
        'faster-whisper': [{ name: 'large-v3', present: true, missing: [] }],
      },
      tts: { silero: [{ name: 'v5_ru', present: true, missing: [] }] },
    },
  },
  sandbox: { url: 'http://localhost:8200', admin_token_set: true, tasks_refill_s: 600, tasks_min: 4 },
};

function renderTab(overrides: Record<string, unknown> = {}) {
  const runtime = { ...RUNTIME, ...overrides };
  return __api.on({
    '/auth/me': ME_ADMIN,
    '/admin/runtime': runtime,
    'PUT /admin/models': (_url: string, init?: RequestInit) =>
      __api.json(200, {
        applied: {
          ...(JSON.parse(String(init?.body ?? '{}')).llm
            ? { llm: { saved: true, applies: 'restart' } }
            : {}),
          voice: { saved: true, status: 200, applies: 'restart' },
        },
        applies: 'restart',
      }),
  });
}

beforeEach(() => {
  localStorage.clear();
  setToken('tok');
  window.location.hash = '#/admin?tab=system';
});

describe('Админка: вкладка «Система»', () => {
  it('показывает подключенное, источник значения и отсутствие ключа в DOM', async () => {
    renderTab();
    render(<AuthProvider><AdminView /></AuthProvider>);

    expect(await screen.findByLabelText('Эндпоинт LLM')).toHaveValue('http://llm:8000/v1');
    expect(screen.getByLabelText('Модель LLM')).toHaveValue('Qwen3.8-Flash');
    // Источник: «переменная» vs «по умолчанию» — видно до сохранения.
    expect(screen.getAllByText(/переменная/).length).toBeGreaterThan(0);
    expect(screen.getAllByText('по умолчанию').length).toBeGreaterThan(0);
    // Ключ — только факт наличия.
    expect(screen.getByText('задан')).toBeInTheDocument();
    expect(document.body.textContent).not.toMatch(/sk-/);
    // Песочница — адрес и режим пополнения.
    expect(screen.getByText('http://localhost:8200')).toBeInTheDocument();
  });

  it('значение из переменной окружения не редактируется из панели', async () => {
    renderTab();
    render(<AuthProvider><AdminView /></AuthProvider>);
    expect(await screen.findByLabelText('Модель LLM')).toBeDisabled();
    expect(screen.getByText('Закреплено переменной — файл её не перебивает')).toBeInTheDocument();
    expect(screen.getByLabelText('Эндпоинт LLM')).toBeEnabled();
  });

  it('модель без весов помечена и объяснена', async () => {
    renderTab();
    render(<AuthProvider><AdminView /></AuthProvider>);
    const sttModel = await screen.findByLabelText('Модель распознавания');
    expect(screen.getByRole('option', { name: 'v3_ctc — нет весов' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'v3_e2e_rnnt' })).toBeInTheDocument();
    await userEvent.selectOptions(sttModel, 'v3_ctc');
    expect(await screen.findByText(/Нет весов: \/models\/gigaam\/v3_ctc.ckpt/)).toBeInTheDocument();
  });

  it('сохранение LLM уходит PUT-ом и честно говорит про перезапуск', async () => {
    const fetchMock = renderTab();
    render(<AuthProvider><AdminView /></AuthProvider>);
    const endpoint = await screen.findByLabelText('Эндпоинт LLM');
    await userEvent.clear(endpoint);
    await userEvent.type(endpoint, 'http://новый:8000/v1');
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить LLM' }));

    expect(await screen.findByText(/применится после перезапуска/)).toBeInTheDocument();
    const put = fetchMock.mock.calls
      .filter(([url, init]) => String(url).includes('/admin/models'))
      .map(([, init]) => init)
      .find((init) => (init?.method ?? '').toUpperCase() === 'PUT');
    expect(put).toBeTruthy();
    expect(JSON.parse(String(put!.body)).llm).toMatchObject({ base_url: 'http://новый:8000/v1' });
  });

  it('без токена voice голоса не сохраняются', async () => {
    renderTab({ voice: { ...RUNTIME.voice, writable: false } });
    render(<AuthProvider><AdminView /></AuthProvider>);
    const btn = await screen.findByRole('button', { name: 'Сохранить голос' });
    expect(btn).toBeDisabled();
    expect(screen.getByText(/VOICE_ADMIN_TOKEN/)).toBeInTheDocument();
  });

  it('недоступный voice — ошибка в блоке, панель живая', async () => {
    renderTab({ voice: { error: 'connect: connection refused' } });
    render(<AuthProvider><AdminView /></AuthProvider>);
    expect(await screen.findByText(/Голосовой сервис не отвечает/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Сохранить LLM' })).toBeEnabled();
    expect(screen.getByRole('button', { name: 'Сохранить голос' })).toBeDisabled();
  });

  it('мок LLM виден отдельным предупреждением', async () => {
    renderTab({ llm: { ...RUNTIME.llm, mock: true } });
    render(<AuthProvider><AdminView /></AuthProvider>);
    expect(await screen.findByText(/ответы заготовлены/)).toBeInTheDocument();
  });
});
