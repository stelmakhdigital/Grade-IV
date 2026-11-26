/**
 * Вкладка «Голоса»: реестр голосов интервьюера (default + список),
 * сохраняется целиком (PUT).
 */
import { useCallback, useEffect, useState } from 'react';
import { adminListTTSVoices, adminPutTTSVoices, type TTSVoiceRegistry, type TTSVoice } from '../../api';
import { errText } from './tabs';

export function VoicesTab() {
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
                <th scope="col">ID</th>
                <th scope="col">Название</th>
                <th scope="col">Пол</th>
                <th scope="col">Спикер (voice)</th>
                <th scope="col"></th>
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

