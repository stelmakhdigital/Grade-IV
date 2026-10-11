/**
 * Admin-UI (итерация 11): редактирование ВСЕХ шаблонов проекта.
 * Три секции: Шаблоны (программы интервью) / Начала разговора / Голоса.
 * Только is_admin (роут #/admin; доступ виден только админу). Дизайн — стили проекта.
 */
import { useCallback, useEffect, useState } from 'react';
import {
  adminListTemplates,
  adminCreateTemplate,
  adminUpdateTemplate,
  adminDeleteTemplate,
  adminListOpenings,
  adminPutOpenings,
  adminListTTSVoices,
  adminPutTTSVoices,
  ApiError,
  type Grade,
  type AdminTemplate,
  type AdminOpening,
  type TemplateBlock,
  type TTSVoiceRegistry,
  type TTSVoice,
} from '../api';
import { useAuth } from '../auth';

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
  const [tab, setTab] = useState<'templates' | 'openings' | 'voices'>('templates');

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
