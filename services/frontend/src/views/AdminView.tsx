/**
 * Admin-UI (итерация 11): редактирование ВСЕХ шаблонов проекта.
 * Три секции: Шаблоны (программы интервью) / Начала разговора / Голоса.
 * Только is_admin (роут #/admin; доступ виден только админу). Дизайн — стили проекта.
 */
import { useCallback, useEffect, useRef, useState } from 'react';
import {
  adminListTemplates,
  adminCreateTemplate,
  adminUpdateTemplate,
  adminDeleteTemplate,
  adminListOpenings,
  adminPutOpenings,
  adminListTTSVoices,
  adminPutTTSVoices,
  adminListTracks,
  adminCreateTrack,
  adminGenerateTrack,
  adminUpdateTrack,
  adminDeleteTrack,
  ApiError,
  type Grade,
  type Stack,
  type AdminTemplate,
  type AdminOpening,
  type AdminTrack,
  type TemplateBlock,
  type TTSVoiceRegistry,
  type TTSVoice,
} from '../api';
import { useAuth } from '../auth';
import { practiceLabel } from '../labels';

const GRADES: Grade[] = ['junior', 'middle', 'senior', 'staff'];
const GRADE_LABEL: Record<string, string> = {
  junior: 'Junior',
  middle: 'Middle',
  senior: 'Senior',
  staff: 'Staff',
  '*': 'Все',
};
const STACKS = [
  { value: '*', label: '*' },
  { value: 'go', label: 'Go' },
  { value: 'python', label: 'Python' },
];

function errText(e: unknown): string {
  if (e instanceof ApiError) return e.message;
  return String(e);
}

function gradeLabel(g: string): string {
  return GRADE_LABEL[g] ?? g;
}

export function AdminView() {
  const { user, logout } = useAuth();
  const [tab, setTab] = useState<'templates' | 'openings' | 'voices' | 'tracks'>('templates');

  // Защита: прямой заход на #/admin без прав — запрет (роут публичный, доступ по is_admin).
  if (user !== null && user.is_admin === false) {
    return (
      <main className="app">
        <h1>Админ</h1>
        <p className="form-error">Доступ запрещён: нужны права администратора.</p>
      </main>
    );
  }

  return (
    <main className="app cabinet">
      <header className="header">
        <div>
          <h1>Админ</h1>
          <p className="muted small">{user?.email}</p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <a className="btn ghost" href="#/">
            В кабинет
          </a>
          <button type="button" className="btn ghost" onClick={logout}>
            Выйти
          </button>
        </div>
      </header>

      <div className="file-tabs" role="tablist">
        {(
          [
            ['templates', 'Шаблоны'],
            ['openings', 'Начала разговора'],
            ['voices', 'Голоса'],
            ['tracks', 'Роли собеседований'],
          ] as const
        ).map(([key, label]) => (
          <button
            key={key}
            type="button"
            role="tab"
            aria-selected={tab === key}
            className={tab === key ? 'tab active' : 'tab'}
            onClick={() => setTab(key)}
          >
            {label}
          </button>
        ))}
      </div>

      {tab === 'templates' && <TemplatesTab />}
      {tab === 'openings' && <OpeningsTab />}
      {tab === 'voices' && <VoicesTab />}
      {tab === 'tracks' && <TracksTab />}
    </main>
  );
}

// ---------------------------------------------------------------- Шаблоны

interface TemplateForm {
  id: number | null;
  name: string;
  grade: Grade;
  stack: string;
  duration_s: number;
  is_default: boolean;
  blocks: TemplateBlock[];
}

const emptyTemplate = (): TemplateForm => ({
  id: null,
  name: '',
  grade: 'middle',
  stack: '*',
  duration_s: 3000,
  is_default: false,
  blocks: [{ title: '', focus: '', question_count: 1 }],
});

function TemplatesTab() {
  const [templates, setTemplates] = useState<AdminTemplate[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [form, setForm] = useState<TemplateForm | null>(null);
  const [saving, setSaving] = useState(false);

  const load = useCallback(async () => {
    try {
      setTemplates(await adminListTemplates());
      setError(null);
    } catch (e) {
      setError(errText(e));
    }
  }, []);
  useEffect(() => {
    void load();
  }, [load]);

  const startEdit = (t: AdminTemplate) =>
    setForm({ id: t.id, name: t.name, grade: t.grade, stack: t.stack, duration_s: t.duration_s, is_default: t.is_default, blocks: t.blocks.map((b) => ({ ...b })) });

  const save = async () => {
    if (!form) return;
    // spec-14/D9: раньше пустое имя/NaN-duration уходили на POST без валидации.
    if (form.name.trim() === '') {
      setError('Укажите название шаблона.');
      return;
    }
    if (!Number.isFinite(form.duration_s) || form.duration_s <= 0) {
      setError('Длительность шаблона — положительное число минут.');
      return;
    }
    setSaving(true);
    setError(null);
    try {
      const body: Omit<AdminTemplate, 'id'> = {
        name: form.name,
        grade: form.grade,
        stack: form.stack,
        duration_s: form.duration_s,
        is_default: form.is_default,
        blocks: form.blocks,
      };
      if (form.id === null) await adminCreateTemplate(body);
      else await adminUpdateTemplate(form.id, body);
      setForm(null);
      await load();
    } catch (e) {
      setError(errText(e));
    } finally {
      setSaving(false);
    }
  };

  const del = async (t: AdminTemplate) => {
    if (!window.confirm(`Удалить шаблон «${t.name}»?`)) return;
    setError(null);
    try {
      await adminDeleteTemplate(t.id);
      await load();
    } catch (e) {
      setError(errText(e));
    }
  };

  const setBlock = (i: number, patch: Partial<TemplateBlock>) =>
    setForm((f) => (f ? { ...f, blocks: f.blocks.map((b, j) => (j === i ? { ...b, ...patch } : b)) } : f));

  return (
    <section className="card">
      <div className="history-head">
        <h2>Шаблоны интервью</h2>
        <button type="button" className="btn primary small" onClick={() => setForm(emptyTemplate())}>
          Новый шаблон
        </button>
      </div>
      {error !== null && <p className="form-error" role="alert">{error}</p>}
      {templates === null && error === null && <p className="muted">Загрузка…</p>}
      {templates !== null && (
        <table className="sessions-table">
          <thead>
            <tr>
              <th>Грейд</th>
              <th>Стек</th>
              <th>Имя</th>
              <th>Дефолт</th>
              <th>Блоки</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {templates.map((t) => (
              <tr key={t.id}>
                <td>{gradeLabel(t.grade)}</td>
                <td>{t.stack}</td>
                <td>{t.name}</td>
                <td>{t.is_default ? 'да' : '—'}</td>
                <td>{t.blocks.length}</td>
                <td>
                  <button type="button" className="btn ghost small" onClick={() => startEdit(t)}>Изменить</button>{' '}
                  <button type="button" className="btn ghost small" onClick={() => void del(t)}>Удалить</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {form !== null && (
        <div className="card" style={{ marginTop: 16, border: '1px solid #2a3140' }}>
          <h3>{form.id === null ? 'Новый шаблон' : `Шаблон #${form.id}`}</h3>
          <div className="start-form">
            <label className="field">
              <span>Имя</span>
              <input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
            </label>
            <label className="field">
              <span>Грейд</span>
              <select value={form.grade} onChange={(e) => setForm({ ...form, grade: e.target.value as Grade })}>
                {GRADES.map((g) => (
                  <option key={g} value={g}>{gradeLabel(g)}</option>
                ))}
              </select>
            </label>
            <label className="field">
              <span>Стек</span>
              <select value={form.stack} onChange={(e) => setForm({ ...form, stack: e.target.value })}>
                {STACKS.map((s) => (
                  <option key={s.value} value={s.value}>{s.label}</option>
                ))}
              </select>
            </label>
            <label className="field">
              <span>Длительность (с)</span>
              <input
                type="number"
                value={form.duration_s}
                onChange={(e) => setForm({ ...form, duration_s: Number(e.target.value) })}
              />
            </label>
            <label className="field" style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
              <input
                type="checkbox"
                checked={form.is_default}
                onChange={(e) => setForm({ ...form, is_default: e.target.checked })}
              />
              <span>Дефолт</span>
            </label>
          </div>
          <p className="muted small">Блоки программы (Название: фокус, N вопросов):</p>
          {form.blocks.map((b, i) => (
            <div key={i} className="start-form">
              <label className="field">
                <span>Название</span>
                <input value={b.title} onChange={(e) => setBlock(i, { title: e.target.value })} />
              </label>
              <label className="field">
                <span>Фокус</span>
                <input value={b.focus} onChange={(e) => setBlock(i, { focus: e.target.value })} />
              </label>
              <label className="field">
                <span>Вопросов</span>
                <input
                  type="number"
                  min={1}
                  value={b.question_count}
                  onChange={(e) => setBlock(i, { question_count: Number(e.target.value) })}
                />
              </label>
              <button
                type="button"
                className="btn ghost small"
                onClick={() => setForm({ ...form, blocks: form.blocks.filter((_, j) => j !== i) })}
              >
                Убрать
              </button>
            </div>
          ))}
          <div style={{ display: 'flex', gap: 8, marginTop: 8 }}>
            <button
              type="button"
              className="btn ghost small"
              onClick={() => setForm({ ...form, blocks: [...form.blocks, { title: '', focus: '', question_count: 1 }] })}
            >
              + Блок
            </button>
            <button type="button" className="btn primary" disabled={saving} onClick={() => void save()}>
              {saving ? 'Сохранение…' : 'Сохранить'}
            </button>
            <button type="button" className="btn ghost" onClick={() => setForm(null)}>
              Отмена
            </button>
          </div>
        </div>
      )}
    </section>
  );
}

// ------------------------------------------------------------- Начала

function OpeningsTab() {
  const [openings, setOpenings] = useState<AdminOpening[] | null>(null);
  const [filter, setFilter] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  const load = useCallback(async () => {
    try {
      setOpenings(await adminListOpenings());
      setError(null);
    } catch (e) {
      setError(errText(e));
    }
  }, []);
  useEffect(() => {
    void load();
  }, [load]);

  const counts: Record<string, number> = {};
  if (openings) for (const o of openings) counts[o.grade] = (counts[o.grade] ?? 0) + 1;

  const visible = openings?.filter((o) => (filter === '' || filter === '*' ? true : o.grade === filter)) ?? [];

  const setRow = (i: number, patch: Partial<AdminOpening>) =>
    setOpenings((prev) => (prev ? prev.map((o, j) => (j === i ? { ...o, ...patch } : o)) : prev));

  const save = async () => {
    if (!openings) return;
    setSaving(true);
    setError(null);
    try {
      await adminPutOpenings(openings);
      await load();
    } catch (e) {
      setError(errText(e));
    } finally {
      setSaving(false);
    }
  };

  return (
    <section className="card">
      <div className="history-head">
        <h2>Начала разговора</h2>
        <div style={{ display: 'flex', gap: 8 }}>
          <button
            type="button"
            className="btn primary small"
            onClick={() => setOpenings((p) => [...(p ?? []), { grade: 'middle', prompt: '' }])}
          >
            Добавить
          </button>
          <button type="button" className="btn primary" disabled={saving} onClick={() => void save()}>
            {saving ? 'Сохранение…' : 'Сохранить все'}
          </button>
        </div>
      </div>
      <p className="muted small">
        Вариантов на грейд:{' '}
        {Object.entries(counts)
          .sort()
          .map(([g, n]) => `${gradeLabel(g)}: ${n}`)
          .join(' · ')}
      </p>
      {error !== null && <p className="form-error" role="alert">{error}</p>}
      <div className="start-form" style={{ marginBottom: 8 }}>
        <label className="field">
          <span>Фильтр по грейду</span>
          <select value={filter} onChange={(e) => setFilter(e.target.value)}>
            <option value="">Все</option>
            {GRADES.map((g) => (
              <option key={g} value={g}>{gradeLabel(g)}</option>
            ))}
          </select>
        </label>
      </div>
      {openings === null && <p className="muted">Загрузка…</p>}
      {openings !== null && (
        <table className="sessions-table">
          <thead>
            <tr>
              <th>Грейд</th>
              <th>Реплика</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {visible.map((o, i) => {
              const realIdx = openings.indexOf(o);
              return (
                <tr key={realIdx}>
                  <td>
                    <select value={o.grade} onChange={(e) => setRow(realIdx, { grade: e.target.value })}>
                      <option value="*">Все</option>
                      {GRADES.map((g) => (
                        <option key={g} value={g}>{gradeLabel(g)}</option>
                      ))}
                    </select>
                  </td>
                  <td>
                    <input value={o.prompt} onChange={(e) => setRow(realIdx, { prompt: e.target.value })} />
                  </td>
                  <td>
                    <button
                      type="button"
                      className="btn ghost small"
                      onClick={() => setOpenings((p) => (p ? p.filter((_, j) => j !== realIdx) : p))}
                    >
                      Удалить
                    </button>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
    </section>
  );
}

// --------------------------------------------------------------- Голоса

function VoicesTab() {
  const [reg, setReg] = useState<TTSVoiceRegistry | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  const load = useCallback(async () => {
    try {
      setReg(await adminListTTSVoices());
      setError(null);
    } catch (e) {
      setError(errText(e));
    }
  }, []);
  useEffect(() => {
    void load();
  }, [load]);

  const setVoice = (i: number, patch: Partial<TTSVoice>) =>
    setReg((r) => (r ? { ...r, voices: r.voices.map((v, j) => (j === i ? { ...v, ...patch } : v)) } : r));

  const save = async () => {
    if (!reg) return;
    // spec-14/D9: «Добавить» создаёт строку с пустыми id/speaker — раньше
    // она уходила на PUT как мусорная запись.
    const bad = reg.voices.find((v) => v.speaker.trim() === '' || v.id.trim() === '');
    if (bad !== undefined) {
      setError('У добавленного голоса заполните id и speaker (поле «speaker» — имя голоса из voice-сервиса).');
      return;
    }
    setSaving(true);
    setError(null);
    try {
      await adminPutTTSVoices(reg);
      await load();
    } catch (e) {
      setError(errText(e));
    } finally {
      setSaving(false);
    }
  };

  return (
    <section className="card">
      <div className="history-head">
        <h2>Голоса интервьюера</h2>
        <div style={{ display: 'flex', gap: 8 }}>
          <button
            type="button"
            className="btn primary small"
            onClick={() => setReg((r) => ({ default: r?.default ?? '', voices: [...(r?.voices ?? []), { id: '', name: '', gender: 'female', speaker: '' }] }))}
          >
            Добавить
          </button>
          <button type="button" className="btn primary" disabled={saving} onClick={() => void save()}>
            {saving ? 'Сохранение…' : 'Сохранить все'}
          </button>
        </div>
      </div>
      {error !== null && <p className="form-error" role="alert">{error}</p>}
      {reg === null && <p className="muted">Загрузка…</p>}
      {reg !== null && (
        <>
          <div className="start-form" style={{ marginBottom: 8 }}>
            <label className="field">
              <span>Голос по умолчанию</span>
              <select value={reg.default} onChange={(e) => setReg({ ...reg, default: e.target.value })}>
                {reg.voices.map((v) => (
                  <option key={v.speaker} value={v.speaker}>
                    {v.name || v.speaker}
                  </option>
                ))}
              </select>
            </label>
          </div>
          <table className="sessions-table">
            <thead>
              <tr>
                <th>ID</th>
                <th>Название</th>
                <th>Пол</th>
                <th>Спикер (voice)</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {reg.voices.map((v, i) => (
                <tr key={v.speaker + i}>
                  <td><input value={v.id} onChange={(e) => setVoice(i, { id: e.target.value })} /></td>
                  <td><input value={v.name} onChange={(e) => setVoice(i, { name: e.target.value })} /></td>
                  <td>
                    <select value={v.gender} onChange={(e) => setVoice(i, { gender: e.target.value })}>
                      <option value="male">male</option>
                      <option value="female">female</option>
                    </select>
                  </td>
                  <td><input value={v.speaker} onChange={(e) => setVoice(i, { speaker: e.target.value })} /></td>
                  <td>
                    <button
                      type="button"
                      className="btn ghost small"
                      onClick={() => setReg((r) => (r ? { ...r, voices: r.voices.filter((_, j) => j !== i) } : r))}
                    >
                      Удалить
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </>
      )}
    </section>
  );
}

// ---------------------------------------------------------------- Треки

// Цвета — hex-эквиваленты палитры DesignPanel (designColors.ts); backend
// хранит group.color как #rrggbb (models/track.go BlockGroup).
const GROUP_COLORS = [
  { value: '', label: 'По умолчанию' },
  { value: '#6eb2f7', label: 'Синий' },
  { value: '#fac938', label: 'Жёлтый' },
  { value: '#3cdd8c', label: 'Зелёный' },
  { value: '#ed5e5e', label: 'Красный' },
  { value: '#b163e9', label: 'Фиолетовый' },
  { value: '#e64c99', label: 'Розовый' },
];

interface TrackGroupRow {
  title: string;
  names: string; // через запятую
  color: string;
}
interface TrackCaseRow {
  grade: Grade;
  title: string;
  statement: string;
  checklist: string; // построчно
}
interface TrackCriterionRow {
  name: string;
  weight: string;
  hint: string;
}
interface TrackFormState {
  isNew: boolean;
  id: string;
  title: string;
  /** spec-22: 1-3 предложения о роли (админу и ИИ; кандидат не видит). */
  description: string;
  /** Практическая стадия (редактор задач): runtime — из банков sandbox. */
  hasPractice: boolean;
  runtime: 'go' | 'python' | 'shell';
  grades: Grade[];
  boardTitle: string;
  reviewPrompt: string;
  groups: TrackGroupRow[];
  cases: TrackCaseRow[];
  criteria: TrackCriterionRow[];
  gradeConfirm: string; // '' — глобальный порог
}

function trackFromApi(t: AdminTrack): TrackFormState {
  return {
    isNew: false,
    id: t.id,
    title: t.title,
    description: t.description ?? '',
    hasPractice: t.practice != null,
    runtime: t.practice?.runtime ?? 'go',
    grades: [...t.grades],
    boardTitle: t.board.title,
    reviewPrompt: t.board.review_prompt,
    groups: t.board.groups.map((g) => ({ title: g.title, names: g.names.join(', '), color: g.color ?? '' })),
    cases: (t.board.cases ?? []).map((c) => ({
      grade: c.grade,
      title: c.title,
      statement: c.statement,
      checklist: (c.checklist ?? []).join('\n'),
    })),
    // AdminTrack.criteria в api.ts описан как пересечение массивов — элемент
    // без hint; локальное сужение до фактической формы ответа API.
    criteria: (t.criteria as { name: string; weight: number; hint?: string }[]).map((c) => ({
      name: c.name,
      weight: String(Number(c.weight.toFixed(4))),
      hint: c.hint ?? '',
    })),
    gradeConfirm: t.grade_confirm === undefined ? '' : String(t.grade_confirm),
  };
}

const emptyTrack = (): TrackFormState => ({
  isNew: true,
  id: '',
  title: '',
  description: '',
  hasPractice: false,
  runtime: 'go',
  grades: ['junior', 'middle', 'senior', 'staff'],
  boardTitle: '',
  reviewPrompt: '',
  groups: [{ title: '', names: '', color: '' }],
  cases: [],
  criteria: [{ name: '', weight: '', hint: '' }],
  gradeConfirm: '',
});

// Сервер нормализует суммы в 0.95..1.05; за окном — 400 с точной причиной.
function weightSum(criteria: TrackCriterionRow[]): number {
  return criteria.reduce((acc, c) => acc + (Number(c.weight) || 0), 0);
}

function buildTrackPayload(f: TrackFormState): AdminTrack {
  const criteria = f.criteria.map((c) => ({
    name: c.name.trim(),
    weight: Number(c.weight) || 0,
    ...(c.hint.trim() === '' ? {} : { hint: c.hint.trim() }),
  }));
  return {
    id: f.id.trim(),
    title: f.title.trim(),
    ...(f.description.trim() === '' ? {} : { description: f.description.trim() }),
    practice: f.hasPractice ? { runtime: f.runtime } : null,
    grades: f.grades,
    board: {
      title: f.boardTitle.trim(),
      review_prompt: f.reviewPrompt,
      groups: f.groups.map((g) => ({
        title: g.title.trim(),
        names: g.names.split(',').map((s) => s.trim()).filter((s) => s !== ''),
        ...(g.color === '' ? {} : { color: g.color }),
      })),
      cases: f.cases
        .filter((c) => c.title.trim() !== '' || c.statement.trim() !== '')
        .map((c) => ({
          grade: c.grade,
          title: c.title.trim(),
          statement: c.statement.trim(),
          checklist: c.checklist.split('\n').map((s) => s.trim()).filter((s) => s !== ''),
        })),
    },
    criteria,
    ...(f.gradeConfirm === '' ? {} : { grade_confirm: Number(f.gradeConfirm) }),
  };
}

function TracksTab() {
  const [tracks, setTracks] = useState<AdminTrack[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [form, setForm] = useState<TrackFormState | null>(null);
  const [saving, setSaving] = useState(false);
  // spec-22: ИИ-заполнение новой роли по описанию.
  const [aiDesc, setAiDesc] = useState('');
  const [aiBusy, setAiBusy] = useState(false);
  const [aiError, setAiError] = useState<string | null>(null);
  const [aiHidden, setAiHidden] = useState(false);

  // spec-20: drag-n-drop палитры — порядок групп (ручка строки) и перенос
  // блоков между группами (чипы превью). Данные драга — в ref: в jsdom
  // DataTransfer не реализован; в браузере чип дополнительно пишет в
  // dataTransfer (Firefox без setData отменяет drag).
  const dragGroup = useRef<number | null>(null); // индекс группы-источника (строки)
  const [dragOverGroup, setDragOverGroup] = useState<number | null>(null);
  const dragChipGroup = useRef<number | null>(null); // группа-источник чипа
  const dragChipName = useRef<string>(''); // имя блока чипа

  /** Переставить группу from на позицию to (DnD строк, дубль стрелок ↑/↓). */
  const moveGroup = (from: number, to: number): void => {
    if (form === null) return;
    setF({
      groups: form.groups.map((x, j, arr) => {
        if (j === to) return arr[from];
        if (j === from) return arr[to];
        return x;
      }),
    });
  };

  /** Перенести блок (чип) из группы from в группу to. */
  const moveChip = (from: number, to: number, name: string): void => {
    if (form === null || from === to || name === '') return;
    const list = (k: number): string[] =>
      form.groups[k].names.split(',').map((s) => s.trim()).filter((s) => s !== '');
    setF({
      groups: form.groups.map((x, k) => {
        if (k === from) return { ...x, names: list(from).filter((n) => n !== name).join(', ') };
        if (k === to) return { ...x, names: [...list(to), name].join(', ') };
        return x;
      }),
    });
  };

  const load = useCallback(async () => {
    try {
      setTracks(await adminListTracks());
      setError(null);
    } catch (e) {
      setError(errText(e));
    }
  }, []);
  useEffect(() => {
    void load();
  }, [load]);

  const startEdit = (t: AdminTrack) => setForm(trackFromApi(t));

  /** spec-22: черновик роли от ИИ → в форму (id админ правит перед сохранением). */
  const generateRole = async (): Promise<void> => {
    setAiBusy(true);
    setAiError(null);
    try {
      const draft = await adminGenerateTrack(aiDesc.trim());
      setForm({ ...trackFromApi(draft), isNew: true });
      setAiHidden(true);
      setAiDesc('');
    } catch (e) {
      // ИИ недоступен/не распарсился — форма остаётся пустой для ручного ввода.
      setAiError(errText(e));
    } finally {
      setAiBusy(false);
    }
  };

  const setF = (patch: Partial<TrackFormState>) => setForm((f) => (f ? { ...f, ...patch } : f));

  const save = async () => {
    if (!form) return;
    // Минимальная клиентская валидация: точные причины — на сервере (400).
    if (form.title.trim() === '') {
      setError('Укажите название трека.');
      return;
    }
    if (form.id.trim() === '') {
      setError('Укажите id трека (строчные буквы, цифры, дефис).');
      return;
    }
    setSaving(true);
    setError(null);
    try {
      const payload = buildTrackPayload(form);
      if (form.isNew) await adminCreateTrack(payload);
      else await adminUpdateTrack(payload);
      setForm(null);
      await load();
    } catch (e) {
      setError(errText(e));
    } finally {
      setSaving(false);
    }
  };

  const del = async (id: string, label: string) => {
    if (!window.confirm(`Удалить трек «${label}»?`)) return;
    setError(null);
    try {
      await adminDeleteTrack(id);
      setForm((f) => (f !== null && !f.isNew && f.id === id ? null : f));
      await load();
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) {
        setError('Трек используется сессиями — удаление невозможно.');
      } else {
        setError(errText(e));
      }
    }
  };

  const sum = form !== null ? weightSum(form.criteria) : 0;
  const sumWarn = form !== null && form.criteria.length > 0 && (sum < 0.95 || sum > 1.05);

  return (
    <section className="card" data-testid="admin-tracks">
      <div className="history-head">
        <h2>Роли собеседований</h2>
        <button
          type="button"
          className="btn primary small"
          data-testid="admin-track-new"
          onClick={() => {
            setAiHidden(false);
            setAiDesc('');
            setAiError(null);
            setForm(emptyTrack());
          }}
        >
          Новая роль
        </button>
      </div>
      {error !== null && (
        <p className="form-error" role="alert">
          {error}
        </p>
      )}
      {tracks === null && error === null && <p className="muted">Загрузка…</p>}
      {tracks !== null && (
        <>
          <div className="start-form">
            <label className="field">
              <span>Выбрать роль для редактирования</span>
              <select
                data-testid="admin-track-select"
                value=""
                onChange={(e) => {
                  const t = tracks.find((x) => x.id === e.target.value);
                  if (t !== undefined) startEdit(t);
                }}
              >
                <option value="">— выберите роль —</option>
                {tracks.map((t) => (
                  <option key={t.id} value={t.id}>
                    {t.title} ({t.id})
                  </option>
                ))}
              </select>
            </label>
          </div>
          <table className="sessions-table">
            <thead>
              <tr>
                <th>ID</th>
                <th>Название</th>
                <th>Тип</th>
                <th>Грейды</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {tracks.map((t) => (
                <tr key={t.id}>
                  <td>{t.id}</td>
                  <td>{t.title}</td>
                  <td>
                    {t.practice != null ? (
                      <span className="badge ok" data-testid={`admin-track-practice-${t.id}`}>
                        практика: {practiceLabel(t.practice.runtime) ?? t.practice.runtime}
                      </span>
                    ) : (
                      <span className="badge">голос и доска</span>
                    )}
                  </td>
                  <td className="muted small">{t.grades.map(gradeLabel).join(', ')}</td>
                  <td>
                    <button type="button" className="btn ghost small" onClick={() => startEdit(t)}>
                      Изменить
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </>
      )}

      {form !== null && (
        <div className="card" style={{ marginTop: 16, border: '1px solid #2a3140' }}>
          <h3>{form.isNew ? 'Новая роль' : `Роль «${form.title || form.id}»`}</h3>
          {/* spec-22: ИИ заполняет конфиг по описанию — блок виден только у
              новой роли, после заполнения/пропуска скрывается. */}
          {form.isNew && !aiHidden && (
            <div className="card" style={{ marginBottom: 14, border: '1px solid #2a3140' }}>
              <label className="field" style={{ marginBottom: 8 }}>
                <span>Опишите роль своими словами — что за специалист и что проверяем</span>
                <textarea
                  rows={2}
                  data-testid="admin-track-ai-desc"
                  value={aiDesc}
                  placeholder="Например: аналитик данных, пишет на Python, строит дашборды и пайплайны"
                  onChange={(e) => setAiDesc(e.target.value)}
                />
              </label>
              <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                <button
                  type="button"
                  className="btn primary small"
                  data-testid="admin-track-ai-fill"
                  disabled={aiBusy || aiDesc.trim().length < 10}
                  onClick={() => void generateRole()}
                >
                  {aiBusy ? 'Заполняю…' : 'Заполнить ИИ'}
                </button>
                <button type="button" className="btn ghost small" data-testid="admin-track-ai-skip" onClick={() => setAiHidden(true)}>
                  Заполнить вручную
                </button>
                {aiError !== null && <span className="form-error">{aiError}</span>}
              </div>
            </div>
          )}
          <div className="start-form">
            <label className="field">
              <span>Название</span>
              <input data-testid="admin-track-title" value={form.title} onChange={(e) => setF({ title: e.target.value })} />
            </label>
            <label className="field">
              <span>ID</span>
              <input value={form.id} disabled={!form.isNew} onChange={(e) => setF({ id: e.target.value })} />
            </label>
            <label className="field" style={{ gridColumn: '1 / -1' }}>
              <span>Описание (1–3 предложения)</span>
              <textarea rows={2} data-testid="admin-track-desc" value={form.description} onChange={(e) => setF({ description: e.target.value })} />
            </label>
            <label className="field" style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
              <input
                type="checkbox"
                data-testid="admin-track-practice"
                checked={form.hasPractice}
                onChange={(e) => setF({ hasPractice: e.target.checked })}
              />
              <span>Практика в редакторе</span>
            </label>
            {form.hasPractice && (
              <label className="field">
                <span>Чем решается</span>
                <select
                  data-testid="admin-track-runtime"
                  value={form.runtime}
                  onChange={(e) => setF({ runtime: e.target.value as TrackFormState['runtime'] })}
                >
                  <option value="go">Go</option>
                  <option value="python">Python</option>
                  <option value="shell">Bash</option>
                </select>
              </label>
            )}
          </div>
          <div className="start-form">
            <label className="field">
              <span>Грейды</span>
              <span style={{ display: 'flex', gap: 12 }}>
                {GRADES.map((g) => (
                  <label key={g} style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
                    <input
                      type="checkbox"
                      checked={form.grades.includes(g)}
                      onChange={(e) =>
                        setF({ grades: e.target.checked ? [...form.grades, g] : form.grades.filter((x) => x !== g) })
                      }
                    />
                    <span>{gradeLabel(g)}</span>
                  </label>
                ))}
              </span>
            </label>
          </div>

          <h4>Доска</h4>
          <div className="start-form">
            <label className="field">
              <span>Заголовок доски</span>
              <input value={form.boardTitle} onChange={(e) => setF({ boardTitle: e.target.value })} />
            </label>
            <label className="field" style={{ gridColumn: '1 / -1' }}>
              <span>Промпт ревью (что оценивать в схеме)</span>
              <textarea
                rows={3}
                value={form.reviewPrompt}
                onChange={(e) => setF({ reviewPrompt: e.target.value })}
              />
            </label>
          </div>

          <p className="muted small">Группы палитры (имена блоков через запятую):</p>
          {/* spec-19/визуальный редактор: живой превью палитры — то же, что
              увидит кандидат на доске (чипы с цветом группы). */}
          <div className="palette-preview" data-testid="admin-track-palette-preview">
            {/* Индексы — оригинальные (пустые группы скрыты, но порядок общий
                со списком строк ниже): DnD чипов переносит блок в группу. */}
            {form.groups.map((g, i) => {
              if (g.title.trim() === '' && g.names.trim() === '') return null;
              return (
                <div
                  key={i}
                  className={`palette-preview-group${dragOverGroup === i ? ' drag-over' : ''}`}
                  data-testid={`admin-track-palette-group-${i}`}
                  onDragOver={(e) => {
                    // подсветка — только при переносе чипа (строки групп
                    // подсвечивает свой onDragOver)
                    if (dragChipGroup.current === null) return;
                    e.preventDefault();
                    setDragOverGroup(i);
                  }}
                  onDrop={(e) => {
                    e.preventDefault();
                    const from = dragChipGroup.current;
                    const name = dragChipName.current;
                    dragChipGroup.current = null;
                    dragChipName.current = '';
                    setDragOverGroup(null);
                    if (from !== null) moveChip(from, i, name);
                  }}
                >
                  <span className="palette-preview-title">{g.title.trim() || 'Без названия'}</span>
                  <span className="palette-preview-chips">
                    {g.names
                      .split(',')
                      .map((s) => s.trim())
                      .filter((s) => s !== '')
                      .map((n, j) => (
                        <span
                          key={j}
                          className="palette-preview-chip"
                          style={g.color !== '' ? { borderColor: g.color } : undefined}
                          draggable
                          data-testid={`admin-track-chip-${i}-${j}`}
                          onDragStart={(e) => {
                            e.dataTransfer.setData('text/plain', `${i}`);
                            dragChipGroup.current = i;
                            dragChipName.current = n;
                          }}
                          onDragEnd={() => {
                            dragChipGroup.current = null;
                            dragChipName.current = '';
                            setDragOverGroup(null);
                          }}
                        >
                          <i style={{ background: g.color !== '' ? g.color : '#6eb2f7' }} />
                          {n}
                        </span>
                      ))}
                  </span>
                </div>
              );
            })}
          </div>
          <div data-testid="admin-track-groups">
            {form.groups.map((g, i) => (
              <div
                key={i}
                className={`start-form${dragOverGroup === i ? ' drag-over' : ''}`}
                data-testid={`admin-track-group-row-${i}`}
                onDragOver={(e) => {
                  if (dragGroup.current === null) return;
                  e.preventDefault();
                  setDragOverGroup(i);
                }}
                onDrop={(e) => {
                  e.preventDefault();
                  const from = dragGroup.current;
                  dragGroup.current = null;
                  setDragOverGroup(null);
                  if (from !== null && from !== i) moveGroup(from, i);
                }}
              >
                <span
                  className="drag-handle"
                  title="Порядок группы — перетащите"
                  aria-label="Перетащить группу"
                  draggable
                  onDragStart={(e) => {
                    e.dataTransfer.setData('text/plain', String(i));
                    dragGroup.current = i;
                  }}
                  onDragEnd={() => {
                    dragGroup.current = null;
                    setDragOverGroup(null);
                  }}
                >
                  ⠿
                </span>
                <label className="field">
                  <span>Группа</span>
                  <input
                    value={g.title}
                    onChange={(e) =>
                      setF({ groups: form.groups.map((x, j) => (j === i ? { ...x, title: e.target.value } : x)) })
                    }
                  />
                </label>
                <label className="field">
                  <span>Имена</span>
                  <input
                    value={g.names}
                    onChange={(e) =>
                      setF({ groups: form.groups.map((x, j) => (j === i ? { ...x, names: e.target.value } : x)) })
                    }
                  />
                </label>
                <label className="field">
                  <span>Цвет</span>
                  <select
                    value={g.color}
                    onChange={(e) =>
                      setF({ groups: form.groups.map((x, j) => (j === i ? { ...x, color: e.target.value } : x)) })
                    }
                  >
                    {GROUP_COLORS.map((c) => (
                      <option key={c.value} value={c.value}>
                        {c.label}
                      </option>
                    ))}
                  </select>
                </label>
                <span style={{ display: 'flex', gap: 4 }}>
                  <button
                    type="button"
                    className="btn ghost small"
                    aria-label="Выше"
                    disabled={i === 0}
                    onClick={() =>
                      setF({
                        groups: form.groups.map((x, j, arr) =>
                          j === i - 1 ? arr[i] : j === i ? arr[i - 1] : x,
                        ),
                      })
                    }
                  >
                    ↑
                  </button>
                  <button
                    type="button"
                    className="btn ghost small"
                    aria-label="Ниже"
                    disabled={i === form.groups.length - 1}
                    onClick={() =>
                      setF({
                        groups: form.groups.map((x, j, arr) =>
                          j === i + 1 ? arr[i] : j === i ? arr[i + 1] : x,
                        ),
                      })
                    }
                  >
                    ↓
                  </button>
                  <button
                    type="button"
                    className="btn ghost small"
                    onClick={() => setF({ groups: form.groups.filter((_, j) => j !== i) })}
                  >
                    Убрать
                  </button>
                </span>
              </div>
            ))}
            <button
              type="button"
              className="btn ghost small"
              onClick={() => setF({ groups: [...form.groups, { title: '', names: '', color: '' }] })}
            >
              + Группа
            </button>
          </div>

          <p className="muted small">Кейсы по грейду (чек-лист — по строке на элемент):</p>
          {form.cases.map((c, i) => (
            <div key={i} className="start-form">
              <label className="field">
                <span>Грейд</span>
                <select
                  value={c.grade}
                  onChange={(e) =>
                    setF({ cases: form.cases.map((x, j) => (j === i ? { ...x, grade: e.target.value as Grade } : x)) })
                  }
                >
                  {GRADES.map((g) => (
                    <option key={g} value={g}>
                      {gradeLabel(g)}
                    </option>
                  ))}
                </select>
              </label>
              <label className="field">
                <span>Название</span>
                <input
                  value={c.title}
                  onChange={(e) => setF({ cases: form.cases.map((x, j) => (j === i ? { ...x, title: e.target.value } : x)) })}
                />
              </label>
              <label className="field" style={{ gridColumn: '1 / -1' }}>
                <span>Задание</span>
                <textarea
                  rows={2}
                  value={c.statement}
                  onChange={(e) =>
                    setF({ cases: form.cases.map((x, j) => (j === i ? { ...x, statement: e.target.value } : x)) })
                  }
                />
              </label>
              <label className="field" style={{ gridColumn: '1 / -1' }}>
                <span>Чек-лист</span>
                <textarea
                  rows={3}
                  value={c.checklist}
                  onChange={(e) =>
                    setF({ cases: form.cases.map((x, j) => (j === i ? { ...x, checklist: e.target.value } : x)) })
                  }
                />
              </label>
              <button
                type="button"
                className="btn ghost small"
                onClick={() => setF({ cases: form.cases.filter((_, j) => j !== i) })}
              >
                Убрать кейс
              </button>
            </div>
          ))}
          <button
            type="button"
            className="btn ghost small"
            onClick={() => setF({ cases: [...form.cases, { grade: 'middle', title: '', statement: '', checklist: '' }] })}
          >
            + Кейс
          </button>

          <h4>Критерии отчёта</h4>
          <table className="sessions-table" data-testid="admin-track-criteria">
            <thead>
              <tr>
                <th>Критерий</th>
                <th>Вес (0..1)</th>
                <th>Подсказка оценщику</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {form.criteria.map((c, i) => (
                <tr key={i}>
                  <td>
                    <input
                      value={c.name}
                      onChange={(e) =>
                        setF({ criteria: form.criteria.map((x, j) => (j === i ? { ...x, name: e.target.value } : x)) })
                      }
                    />
                  </td>
                  <td>
                    <input
                      type="number"
                      step="0.05"
                      min={0}
                      max={1}
                      value={c.weight}
                      onChange={(e) =>
                        setF({
                          criteria: form.criteria.map((x, j) => (j === i ? { ...x, weight: e.target.value } : x)),
                        })
                      }
                    />
                  </td>
                  <td>
                    <input
                      value={c.hint}
                      onChange={(e) =>
                        setF({ criteria: form.criteria.map((x, j) => (j === i ? { ...x, hint: e.target.value } : x)) })
                      }
                    />
                  </td>
                  <td>
                    <button
                      type="button"
                      className="btn ghost small"
                      onClick={() => setF({ criteria: form.criteria.filter((_, j) => j !== i) })}
                    >
                      Убрать
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          <p className={sumWarn ? 'form-error' : 'muted small'}>
            Сумма весов: {sum.toFixed(2)}
            {sumWarn ? ' — вне 0.95..1.05 (сервер нормализует только в этом окне)' : ''}
          </p>
          <button
            type="button"
            className="btn ghost small"
            onClick={() => setF({ criteria: [...form.criteria, { name: '', weight: '', hint: '' }] })}
          >
            + Критерий
          </button>

          <div className="start-form">
            <label className="field">
              <span>Порог подтверждения грейда (1..5, пусто — глобальный)</span>
              <input
                type="number"
                step="0.1"
                min={1}
                max={5}
                value={form.gradeConfirm}
                onChange={(e) => setF({ gradeConfirm: e.target.value })}
              />
            </label>
          </div>

          <div style={{ display: 'flex', gap: 8, marginTop: 8 }}>
            <button type="button" className="btn primary" data-testid="admin-track-save" disabled={saving} onClick={() => void save()}>
              {saving ? 'Сохранение…' : 'Сохранить'}
            </button>
            {!form.isNew && (
              <button
                type="button"
                className="btn danger"
                data-testid="admin-track-delete"
                disabled={saving}
                onClick={() => void del(form.id, form.title)}
              >
                Удалить
              </button>
            )}
            <button type="button" className="btn ghost" onClick={() => setForm(null)}>
              Отмена
            </button>
          </div>
        </div>
      )}
    </section>
  );
}
