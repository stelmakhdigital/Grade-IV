/**
 * Вкладка «Роли собеседований» (spec-18/22): редактор роли — состав стадий,
 * доска (палитра, кейсы), критерии отчёта. Payload строится целиком: сервер
 * сохраняет присланный объект как есть, поэтому не редактируемые в UI поля
 * (board.cases[].required, practice_stacks) прокидываются из загруженной роли.
 */
import { useCallback, useEffect, useRef, useState } from 'react';
import {
  adminListTracks,
  adminCreateTrack,
  adminGenerateTrack,
  adminUpdateTrack,
  adminDeleteTrack,
  ApiError,
  type Grade,
  type Stack,
  type AdminTrack,
} from '../../api';
import { practiceLabel } from '../../labels';
import { ConfirmDialog, type AskConfirm } from '../../confirm';
import { GRADES, errText, gradeLabel } from './tabs';

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
  /** Обязательные артефакты (spec-24). UI их не редактирует, но сервер
   * сохраняет роль как есть — не унесём в payload, исчезнут из базы. */
  required?: string[];
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
  /** Доп. языки кодовой стадии (practice_stacks). Не редактируются в UI —
   * прокидываются из загруженной роли, чтобы сохранение их не теряло. */
  practiceStacks: Stack[];
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
      required: c.required,
    })),
    // AdminTrack.criteria в api.ts описан как пересечение массивов — элемент
    // без hint; локальное сужение до фактической формы ответа API.
    criteria: (t.criteria as { name: string; weight: number; hint?: string }[]).map((c) => ({
      name: c.name,
      weight: String(Number(c.weight.toFixed(4))),
      hint: c.hint ?? '',
    })),
    gradeConfirm: t.grade_confirm === undefined ? '' : String(t.grade_confirm),
    practiceStacks: [...(t.practice_stacks ?? [])],
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
  practiceStacks: [],
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
          // omitempty как в models/track.go: пустое/отсутствующее не отправляем
          ...(c.required !== undefined && c.required.length > 0 ? { required: c.required } : {}),
        })),
    },
    criteria,
    ...(f.gradeConfirm === '' ? {} : { grade_confirm: Number(f.gradeConfirm) }),
    ...(f.practiceStacks.length > 0 ? { practice_stacks: f.practiceStacks } : {}),
  };
}

export function TracksTab() {
  const [tracks, setTracks] = useState<AdminTrack[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [form, setForm] = useState<TrackFormState | null>(null);
  const [saving, setSaving] = useState(false);
  // spec-22: ИИ-заполнение новой роли по описанию.
  const [aiDesc, setAiDesc] = useState('');
  const [aiBusy, setAiBusy] = useState(false);
  const [aiError, setAiError] = useState<string | null>(null);
  const [aiHidden, setAiHidden] = useState(false);
  const [ask, setAsk] = useState<AskConfirm | null>(null);

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
      // 409 (issue #7) — правка практики при незавершённых сессиях роли:
      // программа стадий идущего интервью не должна меняться на середине.
      if (e instanceof ApiError && e.status === 409) {
        setError('У роли есть незавершённые сессии — практику и рантайм меняют, когда интервью не идут.');
      } else {
        setError(errText(e));
      }
    } finally {
      setSaving(false);
    }
  };

  const del = async (id: string, label: string) => {
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

  // issue #7: практика роли — программа стадий её сессий. Сервер не примет её
  // правку, пока у роли есть незавершённые сессии (409); редактор блокирует те
  // же поля по тому же признаку (active_sessions из GET /admin/tracks).
  const busySessions =
    form !== null && !form.isNew ? (tracks?.find((t) => t.id === form.id)?.active_sessions ?? 0) : 0;
  const practiceLocked = busySessions > 0;

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
                <th scope="col">ID</th>
                <th scope="col">Название</th>
                <th scope="col">Практика</th>
                <th scope="col">Грейды</th>
                <th scope="col"></th>
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
                disabled={practiceLocked}
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
                  disabled={practiceLocked}
                  onChange={(e) => setF({ runtime: e.target.value as TrackFormState['runtime'] })}
                >
                  <option value="go">Go</option>
                  <option value="python">Python</option>
                  <option value="shell">Bash</option>
                </select>
              </label>
            )}
            {/* issue #7: блокировка практики активна, только у существующей роли
                с незавершёнными сессиями — то же правило, что на сервере. */}
            {practiceLocked && (
              <p className="muted small" data-testid="admin-track-practice-locked" style={{ gridColumn: '1 / -1' }}>
                Незавершённых сессий у роли: {busySessions}. Идущие интервью живут по своей
                программе стадий — практику и рантайм меняют, когда интервью не идут.
              </p>
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
                  title="Порядок группы — перетащите или кнопками выше/ниже"
                  aria-hidden="true"
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
                <th scope="col">Критерий</th>
                <th scope="col">Вес (0..1)</th>
                <th scope="col">Подсказка оценщику</th>
                <th scope="col">Порядок</th>
                <th scope="col"></th>
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
                    {/* Порядок критериев менялся только перетаскиванием группы;
                        для строк таблицы клавиатурного пути не было вовсе
                        (ui-review 2026-11-27). */}
                    <button
                      type="button"
                      className="btn ghost small"
                      aria-label={`Критерий «${c.name}» выше`}
                      disabled={i === 0}
                      onClick={() =>
                        setF({
                          criteria: form.criteria.map((x, j, arr) =>
                            j === i - 1 ? arr[i] : j === i ? arr[i - 1] : x,
                          ),
                        })
                      }
                    >
                      ↑
                    </button>{' '}
                    <button
                      type="button"
                      className="btn ghost small"
                      aria-label={`Критерий «${c.name}» ниже`}
                      disabled={i === form.criteria.length - 1}
                      onClick={() =>
                        setF({
                          criteria: form.criteria.map((x, j, arr) =>
                            j === i + 1 ? arr[i] : j === i ? arr[i + 1] : x,
                          ),
                        })
                      }
                    >
                      ↓
                    </button>
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
                onClick={() => setAsk({ question: `Удалить трек «${form.title}»?`, run: () => void del(form.id, form.title) })}
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

      {ask !== null && <ConfirmDialog ask={ask} onCancel={() => setAsk(null)} />}
    </section>
  );
}
