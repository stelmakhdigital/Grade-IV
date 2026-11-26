/**
 * Вкладка «Шаблоны»: программы интервью (admin, итерация 11) — список,
 * создание/правка/удаление шаблона с блоками.
 */
import { useCallback, useEffect, useState } from 'react';
import {
  adminListTemplates,
  adminCreateTemplate,
  adminUpdateTemplate,
  adminDeleteTemplate,
  type Grade,
  type AdminTemplate,
  type TemplateBlock,
} from '../../api';
import { ConfirmDialog, type AskConfirm } from '../../confirm';
import { GRADES, STACKS, errText, gradeLabel } from './tabs';

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

export function TemplatesTab() {
  const [templates, setTemplates] = useState<AdminTemplate[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [form, setForm] = useState<TemplateForm | null>(null);
  const [saving, setSaving] = useState(false);
  const [ask, setAsk] = useState<AskConfirm | null>(null);

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
              <th scope="col">Грейд</th>
              <th scope="col">Стек</th>
              <th scope="col">Имя</th>
              <th scope="col">Дефолт</th>
              <th scope="col">Блоки</th>
              <th scope="col"></th>
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
                  <button
                    type="button"
                    className="btn ghost small"
                    onClick={() => setAsk({ question: `Удалить шаблон «${t.name}»?`, run: () => void del(t) })}
                  >
                    Удалить
                  </button>
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

      {ask !== null && <ConfirmDialog ask={ask} onCancel={() => setAsk(null)} />}
    </section>
  );
}

