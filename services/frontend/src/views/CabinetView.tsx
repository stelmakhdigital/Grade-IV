/**
 * Кабинет кандидата (WP-7): профиль + баланс минут,
 * «Новое интервью» (грейд + стек), история сессий со статусами.
 * Активные/паузные сессии ведут в голосовой интерфейс (WP-8) —
 * пока кнопка «Продолжить» ведёт в заглушку, завершённые — в транскрипт.
 */
import { useCallback, useEffect, useState, type FormEvent } from 'react';
import {
  createSession,
  getReport,
  getVacancy,
  listProfiles,
  listSessions,
  listTemplates,
  listTTSVoices,
  saveVacancy,
  ApiError,
  type Grade,
  type InterviewProfile,
  type InterviewTemplate,
  type Session,
  type Stack,
  type TTSVoice,
} from '../api';
import { useAuth } from '../auth';
import { apiErrorMessage } from './LoginView';
import { statusLabel, stageLabel } from '../labels';

const GRADES: { value: Grade; label: string; minutes: number }[] = [
  { value: 'junior', label: 'Junior (45 мин)', minutes: 45 },
  { value: 'middle', label: 'Middle (50 мин)', minutes: 50 },
  { value: 'senior', label: 'Senior (60 мин)', minutes: 60 },
  { value: 'staff', label: 'Staff (75 мин)', minutes: 75 },
];

const STACKS: { value: Stack; label: string }[] = [
  { value: 'go', label: 'Go' },
  { value: 'python', label: 'Python' },
];

/** Путь к странице сессии (hash-роутинг, App.tsx). */
export function sessionPath(id: number): string {
  return `#/sessions/${id}`;
}

/** spec-14/D12: порог паузы сервера (session.DefaultPauseTimeout, SRS §7):
 * пауза дольше — resume сам прерывает сессию (pause_timeout). */
const PAUSE_TIMEOUT_MS = 30 * 60 * 1000;

/** Остаток паузы до порога (мс) для paused-сессии; null — не применимо. */
function pausedLeftMs(s: Session, now: number): number | null {
  if (s.status !== 'paused' || !s.paused_at) return null;
  const t = new Date(s.paused_at).getTime();
  if (Number.isNaN(t)) return null;
  return PAUSE_TIMEOUT_MS - (now - t);
}

export function CabinetView() {
  const { user, minutesRemainingMin, logout, refresh } = useAuth();
  const [sessions, setSessions] = useState<Session[] | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [grade, setGrade] = useState<Grade>('middle');
  const [stack, setStack] = useState<Stack>('go');
  const [templates, setTemplates] = useState<InterviewTemplate[]>([]);
  const [templateId, setTemplateId] = useState<number | undefined>(undefined);
  const [profiles, setProfiles] = useState<InterviewProfile[]>([]);
  const [profileId, setProfileId] = useState<number | undefined>(undefined);
  // F1 (Итерация 10): голос интервьюера (реестр из API, не хардкод).
  const [ttsVoices, setTtsVoices] = useState<TTSVoice[]>([]);
  const [ttsSpeaker, setTtsSpeaker] = useState<string>('');
  const [ttsSpeakerDefault, setTtsSpeakerDefault] = useState<string>('');
  const [starting, setStarting] = useState(false);
  const [startError, setStartError] = useState<string | null>(null);
  // F6 (итерация 12): вакансия/кампания (модалка + индикатор в кабинете).
  const [vacancyOpen, setVacancyOpen] = useState(false);
  const [vacancyText, setVacancyText] = useState<string>('');
  const [vacancyDraft, setVacancyDraft] = useState<string>('');
  const [vacancyBusy, setVacancyBusy] = useState(false);
  const [vacancyError, setVacancyError] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      setSessions(await listSessions());
      setLoadError(null);
    } catch (e) {
      setLoadError(apiErrorMessage(e));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  // spec-14/D13 (F2-progress): тренд прогресса — overall по завершённым
  // сессиям (отчёты грузим лениво, по одной; отсутствие отчёта → null).
  const [overalls, setOveralls] = useState<Record<number, number | null>>({});
  useEffect(() => {
    if (sessions === null) return;
    let cancelled = false;
    const finished = sessions.filter((s) => s.status === 'finished' || s.status === 'aborted');
    (async () => {
      for (const s of finished) {
        try {
          const r = await getReport(s.id);
          if (!cancelled) {
            setOveralls((prev) => ({ ...prev, [s.id]: r === 'generating' ? null : r.overall }));
          }
        } catch {
          if (!cancelled) setOveralls((prev) => ({ ...prev, [s.id]: null }));
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [sessions]);

  // spec-14/D12: тик для пересчёта «осталось паузы» (30 с, дёшево).
  const [nowTick, setNowTick] = useState(() => Date.now());
  useEffect(() => {
    const t = window.setInterval(() => setNowTick(Date.now()), 30000);
    return () => window.clearInterval(t);
  }, []);

  // Шаблоны интервью (итерация A): грузим для выбранного грейда/стека.
  useEffect(() => {
    let cancelled = false;
    listTemplates(grade, stack)
      .then((t) => {
        if (cancelled) return;
        setTemplates(t);
        setTemplateId(t.find((x) => x.is_default)?.id ?? t[0]?.id);
      })
      .catch(() => {
        if (!cancelled) setTemplates([]);
      });
    return () => { cancelled = true; };
  }, [grade, stack]);

  // Профили интервьюера (итерация B): общий список, дефолт — balanced/standard.
  useEffect(() => {
    let cancelled = false;
    listProfiles()
      .then((p) => {
        if (cancelled) return;
        setProfiles(p);
        const def = p.find((x) => x.tone === 'balanced' && x.difficulty === 'standard');
        setProfileId(def?.id ?? p[0]?.id);
      })
      .catch(() => {
        if (!cancelled) setProfiles([]);
      });
    return () => { cancelled = true; };
  }, []);

  // F1 (Итерация 10): реестр голосов интервьюера (селектор перед стартом).
  useEffect(() => {
    let cancelled = false;
    listTTSVoices()
      .then((reg) => {
        if (cancelled) return;
        setTtsVoices(reg.voices ?? []);
        setTtsSpeakerDefault(reg.default ?? '');
        setTtsSpeaker(reg.default || (reg.voices?.[0]?.speaker ?? ''));
      })
      .catch(() => {
        if (!cancelled) setTtsVoices([]);
      });
    return () => { cancelled = true; };
  }, []);

  // F6 (итерация 12): загружаем сохранённую вакансию кандидата (профиль).
  useEffect(() => {
    let cancelled = false;
    getVacancy()
      .then((d) => { if (!cancelled) setVacancyText(d.vacancy_text ?? ''); })
      .catch(() => { if (!cancelled) setVacancyText(''); });
    return () => { cancelled = true; };
  }, []);


  const onStart = async (e: FormEvent) => {
    e.preventDefault();
    if (starting) return;
    setStarting(true);
    setStartError(null);
    try {
      const s = await createSession(grade, stack, templateId, profileId, ttsSpeaker);
      window.location.hash = sessionPath(s.id);
    } catch (err) {
      setStartError(apiErrorMessage(err));
      if (err instanceof ApiError && err.code === 'no_minutes') {
        void refresh();
      }
    } finally {
      setStarting(false);
    }
  };

  // F6 (итерация 12): модалка вакансии — открыть/сохранить/сбросить.
  const openVacancy = () => {
    setVacancyDraft(vacancyText);
    setVacancyError(null);
    setVacancyOpen(true);
  };
  const closeVacancy = () => setVacancyOpen(false);
  const onVacancySave = async () => {
    if (vacancyBusy) return;
    setVacancyBusy(true);
    setVacancyError(null);
    try {
      // Пустая строка = сброс (предсказуемо: saveVacancy('') очищает профиль).
      const d = await saveVacancy(vacancyDraft);
      setVacancyText(d.vacancy_text ?? vacancyDraft);
      setVacancyOpen(false);
    } catch (e) {
      setVacancyError(apiErrorMessage(e));
    } finally {
      setVacancyBusy(false);
    }
  };
  const onVacancyReset = async () => {
    if (vacancyBusy) return;
    setVacancyBusy(true);
    setVacancyError(null);
    try {
      const d = await saveVacancy('');
      setVacancyText(d.vacancy_text ?? '');
      setVacancyDraft('');
    } catch (e) {
      setVacancyError(apiErrorMessage(e));
    } finally {
      setVacancyBusy(false);
    }
  };

  return (
    <main className="app cabinet">
      <header className="header">
        <div>
          <h1>Грейд</h1>
          <p className="muted small">
            <span data-testid="user-email">{user?.email}</span> · минут:{' '}
            <strong data-testid="minutes">{minutesRemainingMin}</strong>
          </p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          {user?.is_admin && (
            <a className="btn ghost" href="#/admin" data-testid="admin-nav">
              Админ
            </a>
          )}
          <button type="button" className="btn ghost" onClick={logout}>
            Выйти
          </button>
        </div>
      </header>
      <section className="card" aria-labelledby="new-interview">
        <h2 id="new-interview">Новое интервью</h2>
        <form className="start-form" onSubmit={onStart}>
          <label className="field">
            <span>Грейды</span>
            <select value={grade} onChange={(e) => setGrade(e.target.value as Grade)}>
              {GRADES.map((g) => (
                <option key={g.value} value={g.value}>
                  {g.label}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            <span>Стек</span>
            <select value={stack} onChange={(e) => setStack(e.target.value as Stack)}>
              {STACKS.map((s) => (
                <option key={s.value} value={s.value}>
                  {s.label}
                </option>
              ))}
            </select>
          </label>
          {templates.length > 1 && (
            <label className="field">
              <span>Шаблон интервью</span>
              <select value={templateId ?? ''} onChange={(e) => setTemplateId(Number(e.target.value))}>
                {templates.map((t) => (
                  <option key={t.id} value={t.id}>
                    {t.name}{t.is_default ? ' (по умолчанию)' : ''}
                  </option>
                ))}
              </select>
            </label>
          )}
          {profiles.length > 0 && (
            <label className="field">
              <span>Профиль интервьюера</span>
              <select value={profileId ?? ''} onChange={(e) => setProfileId(Number(e.target.value))}>
                {profiles.map((p) => (
                  <option key={p.id} value={p.id} title={p.description ?? ''}>
                    {p.name} — {p.tone}/{p.difficulty}
                    {p.tone === 'balanced' && p.difficulty === 'standard' ? ' (по умолчанию)' : ''}
                  </option>
                ))}
              </select>
            </label>
          )}
          {ttsVoices.length > 0 && (
            <label className="field">
              <span>Голос интервьюера</span>
              <select value={ttsSpeaker} onChange={(e) => setTtsSpeaker(e.target.value)} data-testid="tts-voice-select">
                {ttsVoices.map((v) => (
                  <option key={v.id} value={v.speaker}>
                    {v.name}{v.speaker === ttsSpeakerDefault ? ' (по умолчанию)' : ''}
                  </option>
                ))}
              </select>
            </label>
          )}
          {startError !== null && (
            <p className="form-error" role="alert">
              {startError}
            </p>
          )}
          <button
            type="submit"
            className="btn primary"
            disabled={starting}
            data-testid="start-session"
          >
            {starting ? 'Запуск…' : 'Начать интервью'}
          </button>
        </form>
        <div className="vacancy-row">
          <button type="button" className="vacancy-link" onClick={openVacancy} data-testid="vacancy-open">
            Хочу попасть в кампанию
          </button>
          {vacancyText.length > 0 && (
            <span className="vacancy-indicator" data-testid="vacancy-indicator">
              Вакансия: сохранена ({vacancyText.length} симв.)
            </span>
          )}
        </div>
      </section>

      {/* spec-14/D13 (F2-progress): тренд итоговых баллов по завершённым
          интервью (хронология слева направо — как в истории сверху вниз). */}
      {(() => {
        const rows = (sessions ?? [])
          .filter((s) => overalls[s.id] != null)
          .reverse();
        if (rows.length < 2) return null;
        const first = overalls[rows[0].id] as number;
        const last = overalls[rows[rows.length - 1].id] as number;
        const delta = last - first;
        const arrow = delta > 0.05 ? '↗' : delta < -0.05 ? '↘' : '→';
        return (
          <section className="card trend-card" aria-labelledby="trend">
            <div className="history-head">
              <h2 id="trend">Прогресс по итогам</h2>
              <span className="trend-delta" data-testid="trend-delta">
                {arrow} {delta >= 0 ? '+' : ''}{delta.toFixed(2)}{rows.length > 2 ? ` (за ${rows.length} интервью)` : ''}
              </span>
            </div>
            <div className="trend-row" data-testid="trend-row">
              {rows.map((s, i) => {
                const v = overalls[s.id] as number;
                const cls = v >= 4 ? 'good' : v >= 2.5 ? 'mid' : 'bad';
                const prev = i > 0 ? (overalls[rows[i - 1].id] as number) : null;
                const diff = prev !== null ? v - prev : null;
                return (
                  <span
                    key={s.id}
                    className={`trend-chip ${cls}`}
                    title={`${formatDate(s.started_at)} · ${s.grade} / ${s.stack}${diff !== null ? ` · к прошлому: ${diff >= 0 ? '+' : ''}${diff.toFixed(2)}` : ''}`}
                  >
                    {v.toFixed(1)}
                  </span>
                );
              })}
            </div>
            <p className="muted small">Итоговый балл интервью (1–5). Хронология слева направо.</p>
          </section>
        );
      })()}

      <section className="card" aria-labelledby="history">
        <div className="history-head">
          <h2 id="history">История</h2>
          <button type="button" className="btn ghost small" onClick={() => void load()}>
            Обновить
          </button>
        </div>
        {loadError !== null && <p className="form-error">{loadError}</p>}
        {sessions === null && loadError === null && <p className="muted">Загрузка…</p>}
        {sessions !== null && sessions.length === 0 && (
          <p className="muted">Сессий пока нет — начните первое интервью.</p>
        )}
        {sessions !== null && sessions.length > 0 && (
          <table className="sessions-table">
            <thead>
              <tr>
                <th>Дата</th>
                <th>Грейд</th>
                <th>Стек</th>
                <th>Стадия</th>
                <th>Статус</th>
                <th>Длительность</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {sessions.map((s) => (
                <tr key={s.id} data-session-status={s.status}>
                  <td>{formatDate(s.started_at)}</td>
                  <td>{s.grade}</td>
                  <td>{s.stack}</td>
                  <td>{stageLabel(s.stage)}</td>
                  <td>
                    <span className={`status ${s.status}`}>{statusLabel(s.status)}</span>
                    {/* spec-14/D13: итоговый балл рядом со статусом. */}
                    {overalls[s.id] != null && (
                      <span
                        className={`score-chip ${(overalls[s.id] as number) >= 4 ? 'good' : (overalls[s.id] as number) >= 2.5 ? 'mid' : 'bad'}`}
                        title="Итоговый балл отчёта (1–5)"
                        data-testid={`overall-${s.id}`}
                      >
                        {(overalls[s.id] as number).toFixed(1)}
                      </span>
                    )}
                    {/* spec-14/D12: пауза дольше порога — «Продолжить» прервёт
                        интервью (pause_timeout) — предупреждаем заранее. */}
                    {(() => {
                      const left = pausedLeftMs(s, nowTick);
                      if (left === null) return null;
                      if (left <= 0) {
                        return (
                          <span
                            className="paused-warn danger"
                            data-testid={`pause-warn-${s.id}`}
                            title="Пауза дольше порога (30 мин): «Продолжить» завершит интервью и запустит генерацию отчёта"
                          >
                            порог паузы истёк
                          </span>
                        );
                      }
                      if (left < 10 * 60 * 1000) {
                        return (
                          <span
                            className="paused-warn"
                            data-testid={`pause-warn-${s.id}`}
                            title="Пауза дольше порога (30 мин) прервёт интервью"
                          >
                            паузы осталось {Math.ceil(left / 60000)} мин
                          </span>
                        );
                      }
                      return null;
                    })()}
                  </td>
                  <td>{formatSeconds(s.active_seconds)}</td>
                  <td>
                    {s.status === 'finished' || s.status === 'aborted' ? (
                      <a className="btn ghost small" href={sessionPath(s.id)}>
                        Транскрипт
                      </a>
                    ) : (
                      <a className="btn ghost small" href={sessionPath(s.id)}>
                        Продолжить
                      </a>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>

      {vacancyOpen && (
        <div
          className="modal-overlay"
          onClick={(e) => { if (e.target === e.currentTarget) closeVacancy(); }}
        >
          {/* spec-14/D9: клавиатурная доступность — Escape закрывает (focus
              внутри диалога после autoFocus), описание связано по id. */}
          <div
            className="modal-box"
            role="dialog"
            aria-modal="true"
            aria-labelledby="vacancy-title"
            aria-describedby="vacancy-desc"
            onKeyDown={(e) => {
              if (e.key === 'Escape' && !vacancyBusy) closeVacancy();
            }}
          >
            <div className="modal-head">
              <h3 id="vacancy-title">Хочу попасть в кампанию</h3>
              <button type="button" className="btn ghost small" onClick={closeVacancy} aria-label="Закрыть">
                ✕
              </button>
            </div>
            <p className="modal-desc" id="vacancy-desc">
              Вставьте описание реальной вакансии, в которую хотите попасть. ИИ-интервьюер
              проведёт собеседование так, будто он из этой компании: будет отталкиваться от
              стека, процессов и контекста вакансии, адаптируя глубину под выбранный грейд.
            </p>
            <textarea
              className="modal-textarea"
              value={vacancyDraft}
              onChange={(e) => setVacancyDraft(e.target.value)}
              placeholder="Описание вакансии: компания, стек, задачи, требования…"
              maxLength={20000}
              autoFocus
              data-testid="vacancy-textarea"
            />
            <div className="muted small" style={{ marginTop: 6 }} data-testid="vacancy-counter">
              {vacancyDraft.length} / 20000
            </div>
            {vacancyError !== null && (
              <p className="form-error" role="alert">{vacancyError}</p>
            )}
            <div className="modal-actions">
              <button
                type="button"
                className="btn danger"
                onClick={() => void onVacancyReset()}
                disabled={vacancyBusy || vacancyDraft === ''}
                data-testid="vacancy-reset"
              >
                Сбросить
              </button>
              <div className="spacer" />
              <button type="button" className="btn" onClick={closeVacancy} disabled={vacancyBusy}>
                Отмена
              </button>
              <button
                type="button"
                className="btn primary"
                onClick={() => void onVacancySave()}
                disabled={vacancyBusy}
                data-testid="vacancy-save"
              >
                {vacancyBusy ? 'Сохранение…' : 'Сохранить'}
              </button>
            </div>
          </div>
        </div>
      )}
    </main>
  );
}

function formatDate(iso: string): string {
  const d = new Date(iso);
  return d.toLocaleString('ru-RU', {
    day: '2-digit',
    month: '2-digit',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}

function formatSeconds(total: number): string {
  const m = Math.floor(total / 60);
  const s = Math.round(total % 60);
  return `${m} мин ${s.toString().padStart(2, '0')} с`;
}
