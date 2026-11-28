/**
 * Общий каркас вкладок админки: role=tablist (стрелки/Home/End — через
 * tablistKeyDown, активная вкладка следует за фокусом), разметка переключения
 * и общие мелочи (грейды, подписи, текст ошибки), разделяемые вкладками.
 */
import { ApiError, type Grade } from '../../api';
import { tablistKeyDown } from '../../hooks/tablistKeys';
import { TemplatesTab } from './templates';
import { OpeningsTab } from './openings';
import { VoicesTab } from './voices';
import { TracksTab } from './tracks';

export const ADMIN_TABS = ['templates', 'openings', 'voices', 'tracks'] as const;
export type AdminTab = (typeof ADMIN_TABS)[number];

const TAB_LABELS: Record<AdminTab, string> = {
  templates: 'Шаблоны',
  openings: 'Начала разговора',
  voices: 'Голоса',
  tracks: 'Роли собеседований',
};

/** Вкладки + панель: содержимое активной вкладки монтируется при выборе. */
export function AdminTabs({ tab, onSelect }: { tab: AdminTab; onSelect: (t: AdminTab) => void }) {
  return (
    <>
      <div
        className="file-tabs"
        role="tablist"
        aria-label="Разделы админки"
        onKeyDown={tablistKeyDown({
          index: ADMIN_TABS.indexOf(tab),
          count: ADMIN_TABS.length,
          onSelect: (i) => onSelect(ADMIN_TABS[i]),
        })}
      >
        {ADMIN_TABS.map((key) => (
          <button
            key={key}
            type="button"
            role="tab"
            id={`admin-tab-${key}`}
            aria-controls="admin-panel"
            aria-selected={tab === key}
            className={tab === key ? 'tab active' : 'tab'}
            onClick={() => onSelect(key)}
          >
            {TAB_LABELS[key]}
          </button>
        ))}
      </div>

      <div id="admin-panel" role="tabpanel" aria-labelledby={`admin-tab-${tab}`}>
        {tab === 'templates' && <TemplatesTab />}
        {tab === 'openings' && <OpeningsTab />}
        {tab === 'voices' && <VoicesTab />}
        {tab === 'tracks' && <TracksTab />}
      </div>
    </>
  );
}

// ------------------------------------------------------------- общие мелочи

export const GRADES: Grade[] = ['junior', 'middle', 'senior', 'staff'];

const GRADE_LABEL: Record<string, string> = {
  junior: 'Junior',
  middle: 'Middle',
  senior: 'Senior',
  staff: 'Staff',
  '*': 'Все',
};
export const STACKS = [
  { value: '*', label: 'любой' },
  { value: 'go', label: 'Go' },
  { value: 'python', label: 'Python' },
];

export function errText(e: unknown): string {
  if (e instanceof ApiError) return e.message;
  return String(e);
}

export function gradeLabel(g: string): string {
  return GRADE_LABEL[g] ?? g;
}

/**
 * Минимальная конфигурация стенда для тестов вкладок: сами вкладки про неё
 * ничего не знают, но экран настроек без неё показывал бы ошибку вместо
 * редактора. Значения — те, что даёт стенд без переопределений.
 */
export const SETTINGS_STUB = {
  app_env: 'dev',
  applies: 'restart',
  applies_note: '',
  writable: true,
  paths: { app_file: 'config/app.json' },
  llm: {
    base_url: { value: 'http://llm:8000/v1', source: 'default', env: 'LLM_BASE_URL' },
    model: { value: '', source: 'default', env: 'LLM_MODEL' },
    enable_thinking: { value: false, source: 'default', env: 'LLM_ENABLE_THINKING' },
    mock: false,
    api_key_set: true,
    config_path: 'config/app.json',
  },
  voice: {
    address: { value: 'http://127.0.0.1:8100', source: 'default', env: 'VOICE_URL' },
    current: {
      stt: { provider: 'gigaam', model: 'v3_e2e_rnnt', loaded: true },
      tts: { provider: 'silero', model: 'v5_ru', loaded: true, speakers: ['xenia'] },
    },
    catalog: {},
  },
  sandbox: { url: { value: 'http://127.0.0.1:8200', source: 'default', env: 'SANDBOX_URL' }, admin_token_set: false, tasks_refill_s: 600, tasks_min: 4 },
  timing: {},
  install: { database_url: 'sqlite', editable: false, note: '' },
  pending: [],
};
