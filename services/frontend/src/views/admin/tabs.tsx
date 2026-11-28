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
import { SystemTab } from './system';

export const ADMIN_TABS = ['templates', 'openings', 'voices', 'tracks', 'system'] as const;
export type AdminTab = (typeof ADMIN_TABS)[number];

const TAB_LABELS: Record<AdminTab, string> = {
  templates: 'Шаблоны',
  openings: 'Начала разговора',
  voices: 'Голоса',
  tracks: 'Роли собеседований',
  system: 'Система',
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
        {tab === 'system' && <SystemTab />}
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
