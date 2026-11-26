/**
 * Вкладка «Начала разговора»: реплики-начала по грейдам, сохраняются
 * списком целиком (PUT).
 */
import { useCallback, useEffect, useState } from 'react';
import { adminListOpenings, adminPutOpenings, type AdminOpening } from '../../api';
import { GRADES, errText, gradeLabel } from './tabs';

export function OpeningsTab() {
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
              <th scope="col">Грейд</th>
              <th scope="col">Реплика</th>
              <th scope="col"></th>
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

