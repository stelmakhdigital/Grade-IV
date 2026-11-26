/**
 * Кабинет кандидата: резюме (описание + файл) вместо выбора роли и грейда,
 * профиль/голос интервьюера, история сессий, вакансия кампании.
 *
 * Роль собеседования и грейд подбирает ИИ по резюме (POST /sessions без grade):
 * базовый грейд — тот, что резюме подтверждает, цель интервью — на шаг выше.
 * Время интервью — метрика (длительность в истории), а не валюта: баланса
 * минут в кабинете больше нет.
 */
import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react';
import {
  createSession,
  deleteResume,
  getReport,
  getResume,
  getVacancy,
  listProfiles,
  listSessions,
  listTTSVoices,
  listTracks,
  saveResume,
  saveVacancy,
  ApiError,
  type InterviewProfile,
  type ResumeInfo,
  type Session,
  type Track,
  type TTSVoice,
} from '../api';
import { useAuth } from '../auth';
import { useDialog } from '../hooks/useDialog';
import { useHashQuery } from '../hooks/useHashQuery';
import { num } from '../format';
import { apiErrorMessage } from './LoginView';
import { humanSeconds, practiceLabel, statusLabel, stageLabel } from '../labels';

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

/** Пустое состояние резюме (пока не пришли данные). */
const NO_RESUME: ResumeInfo = { about: '', file_name: '', text_chars: 0 };

/** Страница истории (ui-review 2026-11-27). */
const HIST_PAGE = 20;

export function CabinetView() {
  const { user, logout } = useAuth();
  const [sessions, setSessions] = useState<Session[] | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  // spec-20: фильтр истории (все/активные/завершённые). Живёт в URL (#/?f=active),
  // чтобы F5 и «Назад» его сохраняли (ui-review 2026-11-27).
  const [query, setQuery] = useHashQuery();
  const histFilter: 'all' | 'active' | 'finished' =
    query.get('f') === 'active' ? 'active' : query.get('f') === 'finished' ? 'finished' : 'all';
  // История постраничная: без лимита лента у пользователя с сотней интервью
  // renderится целиком.
  const [histShown, setHistShown] = useState(HIST_PAGE);
  const setHistFilter = (f: 'all' | 'active' | 'finished'): void => {
    setQuery({ f: f === 'all' ? '' : f });
    setHistShown(HIST_PAGE);
  };
  // Имена ролей для истории (роль выбирает ИИ — подписи берём из /tracks).
  const [tracks, setTracks] = useState<Track[]>([]);
  useEffect(() => {
    let alive = true;
    listTracks()
      .then((ts) => {
        if (alive) setTracks(ts);
      })
      .catch(() => {
        /* список ролей необязателен: в истории останется id роли */
      });
    return () => {
      alive = false;
    };
  }, []);

  // spec-20: фильтр истории. «Активные» = active|paused, «Завершённые» =
  // finished|aborted (прекращённые тоже past).
  const visibleSessions = (sessions ?? []).filter((s) => {
    if (histFilter === 'active') return s.status === 'active' || s.status === 'paused';
    if (histFilter === 'finished') return s.status === 'finished' || s.status === 'aborted';
    return true;
  });
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
  // Резюме: описание + файл (pdf/docx/doc) — по ним ИИ берёт роль и грейд.
  const [resumeOpen, setResumeOpen] = useState(false);
  const [resume, setResume] = useState<ResumeInfo>(NO_RESUME);
  const [resumeAbout, setResumeAbout] = useState('');
  const [resumeFile, setResumeFile] = useState<File | null>(null);
  const [resumeBusy, setResumeBusy] = useState(false);
  const [resumeError, setResumeError] = useState<string | null>(null);
  // Доступность диалогов: ловушка Tab, Escape из любой точки, возврат фокуса
  // на открывшую кнопку (ui-review 2026-11-24).
  const vacancyBoxRef = useRef<HTMLDivElement | null>(null);
  const resumeBoxRef = useRef<HTMLDivElement | null>(null);
  const dialogTriggerRef = useRef<HTMLElement | null>(null);
  useDialog({
    open: vacancyOpen,
    onClose: () => setVacancyOpen(false),
    boxRef: vacancyBoxRef,
    returnFocusTo: dialogTriggerRef,
  });
  useDialog({
    open: resumeOpen,
    onClose: () => setResumeOpen(false),
    boxRef: resumeBoxRef,
    returnFocusTo: dialogTriggerRef,
  });

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

  // Резюме кандидата (описание + файл): по нему подбираются роль и грейд.
  useEffect(() => {
    let cancelled = false;
    getResume()
      .then((r) => {
        if (cancelled) return;
        setResume(r);
        setResumeAbout(r.about ?? '');
      })
      .catch(() => {
        /* резюме необязательно: модалка откроется с пустой формой */
      });
    return () => { cancelled = true; };
  }, []);

  const onStart = async (e: FormEvent) => {
    e.preventDefault();
    if (starting) return;
    setStarting(true);
    setStartError(null);
    try {
      // Роль и грейд не отправляем: их подбирает ИИ по резюме.
      const s = await createSession({ profileId, ttsSpeaker });
      window.location.hash = sessionPath(s.id);
    } catch (err) {
      setStartError(apiErrorMessage(err));
    } finally {
      setStarting(false);
    }
  };

  // F6 (итерация 12): модалка вакансии — открыть/сохранить/сбросить.
  const openVacancy = () => {
    dialogTriggerRef.current = document.activeElement as HTMLElement | null;
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

  const openResume = () => {
    dialogTriggerRef.current = document.activeElement as HTMLElement | null;
    setResumeAbout(resume.about ?? '');
    setResumeFile(null);
    setResumeError(null);
    setResumeOpen(true);
  };
  const closeResume = () => setResumeOpen(false);
  const onResumeSave = async () => {
    if (resumeBusy) return;
    setResumeBusy(true);
    setResumeError(null);
    try {
      setResume(await saveResume(resumeAbout, resumeFile));
      setResumeOpen(false);
    } catch (e) {
      setResumeError(apiErrorMessage(e));
    } finally {
      setResumeBusy(false);
    }
  };
  const onResumeReset = async () => {
    if (resumeBusy) return;
    setResumeBusy(true);
    setResumeError(null);
    try {
      setResume(await deleteResume());
      setResumeAbout('');
      setResumeFile(null);
    } catch (e) {
      setResumeError(apiErrorMessage(e));
    } finally {
      setResumeBusy(false);
    }
  };

  // Время интервью — метрика (не валюта): суммарная длительность всех сессий.
  const totalSeconds = (sessions ?? []).reduce((acc, s) => acc + s.active_seconds, 0);

  return (
    <main className="app cabinet">
      <header className="header">
        <div>
          <h1>Грейд</h1>
          <p className="muted small">
            <span data-testid="user-email">{user?.email}</span>
            {sessions !== null && (
              <>
                {' '}· интервью: {sessions.length}
                {/* Длительность интервью — метрика, а не списываемый баланс. */}
                {totalSeconds > 0 && (
                  <span data-testid="total-time"> · суммарно {humanSeconds(totalSeconds)}</span>
                )}
              </>
            )}
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
          {/* Фидбек 2026-11-22: все контролы ряда подписаны сверху и выровнены
              по нижней линии (`.start-form` — flex, align-items: flex-end),
              статус резюме / ошибка / бриф — отдельными строками. */}
          <div className="field resume-field" data-testid="resume-row">
            <span>Резюме</span>
            <button type="button" className="btn" onClick={openResume} data-testid="resume-open">
              Загрузить…
            </button>
          </div>
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
          <button
            type="submit"
            className="btn primary"
            disabled={starting}
            data-testid="start-session"
          >
            {starting ? 'Запуск…' : 'Начать интервью'}
          </button>
          {startError !== null && (
            <p className="form-error" role="alert">
              {startError}
            </p>
          )}
          <p className="resume-state small" data-testid="resume-indicator">
            {resume.file_name
              ? `${resume.file_name} · ${resume.text_chars} симв. из файла`
              : resume.about
                ? `описание: ${resume.about.length} симв.`
                : 'не заполнено — роль и сложность подберём по нему'}
          </p>
          {/* spec-21 (Codility: intro до запуска таймера): короткий бриф —
              что будет на интервью, чтобы старт не был сюрпризом. */}
          <p className="start-brief muted small" data-testid="start-brief">
            Голосовое интервью с ИИ: говорите в микрофон. Практика — в редакторе,
            на клавиатуре, если она есть для подобранной роли. Схему рисуете на доске.
            Таймер и запись включатся после старта. Роль и сложность интервьюер
            подберёт по резюме и будет держать планку на шаг выше подтверждённого уровня.
          </p>
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
                {arrow} {delta >= 0 ? '+' : ''}{num(delta)}{rows.length > 2 ? ` (за ${rows.length} интервью)` : ''}
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
                    title={`${formatDate(s.started_at)} · ${s.grade} / ${s.stack}${diff !== null ? ` · к прошлому: ${diff >= 0 ? '+' : ''}${num(diff)}` : ''}`}
                  >
                    {num(v, 1)}
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
          {/* spec-20: фильтр истории — без него длинная лента нечитаема. */}
          <div className="history-filter" role="group" aria-label="Фильтр истории">
            {([['all', 'Все'], ['active', 'Активные'], ['finished', 'Завершённые']] as const).map(([key, label]) => (
              <button
                key={key}
                type="button"
                className={`chip small${histFilter === key ? ' chip-active' : ''}`}
                aria-pressed={histFilter === key}
                onClick={() => setHistFilter(key)}
                data-testid={`hist-filter-${key}`}
              >
                {label}
              </button>
            ))}
          </div>
          <button type="button" className="btn ghost small" onClick={() => void load()}>
            Обновить
          </button>
        </div>
        {loadError !== null && <p className="form-error" role="alert">{loadError}</p>}
        {sessions === null && loadError === null && <p className="muted">Загрузка…</p>}
        {sessions !== null && sessions.length === 0 && (
          <p className="muted">Сессий пока нет — заполните резюме и начните первое интервью.</p>
        )}
        {sessions !== null && sessions.length > 0 && visibleSessions.length === 0 && (
          <p className="muted" data-testid="hist-empty-filtered">В этом фильтре сессий нет.</p>
        )}
        {sessions !== null && visibleSessions.length > 0 && (
          <table className="sessions-table">
            <thead>
              <tr>
                <th scope="col">Дата</th>
                <th scope="col">Роль</th>
                <th scope="col">Уровень</th>
                <th scope="col">Стадия</th>
                <th scope="col">Статус</th>
                <th scope="col">Длительность</th>
                <th scope="col"></th>
              </tr>
            </thead>
            <tbody>
              {visibleSessions.slice(0, histShown).map((s) => {
                const sTrack = tracks.find((t) => t.id === s.track_id);
                return (
                <tr key={s.id} data-session-status={s.status}>
                  <td>{formatDate(s.started_at)}</td>
                  {/* spec-20: роль видна в истории (раньше только грейд); shell-стадия — «Практика». */}
                  <td>{sTrack ? sTrack.title : (s.track_id || '—')}</td>
                  {/* Цель интервью + база по резюме («собесовали на шаг выше»). */}
                  <td title={s.base_grade ? `по резюме: ${s.base_grade}` : undefined}>
                    {s.grade}
                    {s.base_grade && s.base_grade !== s.grade ? ` (по резюме ${s.base_grade})` : ''}
                  </td>
                  <td>
                    {s.stage === 'livecode' && practiceLabel(sTrack?.practice?.runtime) !== null
                      ? `Практика (${practiceLabel(sTrack?.practice?.runtime)})`
                      : stageLabel(s.stage)}
                  </td>
                  <td>
                    <span className={`status ${s.status}`}>{statusLabel(s.status)}</span>
                    {/* spec-14/D13: итоговый балл рядом со статусом. */}
                    {overalls[s.id] != null && (
                      <span
                        className={`score-chip ${(overalls[s.id] as number) >= 4 ? 'good' : (overalls[s.id] as number) >= 2.5 ? 'mid' : 'bad'}`}
                        title="Итоговый балл отчёта (1–5)"
                        data-testid={`overall-${s.id}`}
                      >
                        {num(overalls[s.id] as number, 1)}
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
                            пауза: осталось {Math.ceil(left / 60000)} мин
                          </span>
                        );
                      }
                      return null;
                    })()}
                  </td>
                  <td>{formatSeconds(s.active_seconds)}</td>
                  <td>
                    {/* spec-20: ссылка ведёт на ОТЧЁТ сессии, а не транскрипт. */}
                    {s.status === 'finished' || s.status === 'aborted' ? (
                      <a className="btn ghost small" href={sessionPath(s.id)}>
                        Отчёт
                      </a>
                    ) : (
                      <a className="btn ghost small" href={sessionPath(s.id)}>
                        Продолжить
                      </a>
                    )}
                  </td>
                </tr>
                );
              })}
            </tbody>
          </table>
        )}
        {/* Покрупнее страницы по клику — иначе лента на сотни строк. */}
        {visibleSessions.length > histShown && (
          <p>
            <button
              type="button"
              className="btn ghost small"
              onClick={() => setHistShown((n) => n + HIST_PAGE)}
              data-testid="hist-more"
            >
              Показать ещё {Math.min(HIST_PAGE, visibleSessions.length - histShown)} из{' '}
              {visibleSessions.length - histShown}
            </button>
          </p>
        )}
      </section>

      {resumeOpen && (
        <div
          className="modal-overlay"
          onClick={(e) => { if (e.target === e.currentTarget) closeResume(); }}
        >
          <div
            ref={resumeBoxRef}
            className="modal-box"
            role="dialog"
            aria-modal="true"
            aria-labelledby="resume-title"
            aria-describedby="resume-desc"
          >
            <div className="modal-head">
              <h3 id="resume-title">Резюме</h3>
              <button type="button" className="btn ghost small" onClick={closeResume} aria-label="Закрыть">
                ✕
              </button>
            </div>
            <p className="modal-desc" id="resume-desc">
              Расскажите о себе и приложите файл резюме. По ним интервьюер решит, на какой
              роли и уровне вас собеседовать, и будет держать планку на шаг выше
              подтверждённого уровня (если не потянете — снизит и отметит это в отчёте).
            </p>
            <textarea
              className="modal-textarea"
              value={resumeAbout}
              onChange={(e) => setResumeAbout(e.target.value)}
              placeholder="Кто вы: стек, годы опыта, последние проекты, над чем хотите расти…"
              maxLength={20000}
              autoFocus
              data-testid="resume-about"
            />
            <div className="muted small" style={{ marginTop: 6 }} data-testid="resume-counter">
              {resumeAbout.length} / 20000
            </div>
            <label className="field resume-file">
              <span>Файл резюме (pdf, docx, doc — до 8 МБ)</span>
              <input
                type="file"
                accept=".pdf,.docx,.doc,application/pdf,application/vnd.openxmlformats-officedocument.wordprocessingml.document,application/msword"
                onChange={(e) => setResumeFile(e.target.files?.[0] ?? null)}
                data-testid="resume-file"
              />
              <span className="muted small" data-testid="resume-current">
                {resumeFile
                  ? `будет загружен: ${resumeFile.name}`
                  : resume.file_name
                    ? `сейчас приложен: ${resume.file_name} (${resume.text_chars} симв. текста)`
                    : 'файл не приложен'}
              </span>
            </label>
            {resumeError !== null && (
              <p className="form-error" role="alert">{resumeError}</p>
            )}
            <div className="modal-actions">
              <button
                type="button"
                className="btn danger"
                onClick={() => void onResumeReset()}
                disabled={resumeBusy || (!resume.about && !resume.file_name)}
                data-testid="resume-reset"
              >
                Сбросить
              </button>
              <div className="spacer" />
              <button type="button" className="btn" onClick={closeResume} disabled={resumeBusy}>
                Отмена
              </button>
              <button
                type="button"
                className="btn primary"
                onClick={() => void onResumeSave()}
                disabled={resumeBusy}
                data-testid="resume-save"
              >
                {resumeBusy ? 'Сохранение…' : 'Сохранить'}
              </button>
            </div>
          </div>
        </div>
      )}

      {vacancyOpen && (
        <div
          className="modal-overlay"
          onClick={(e) => { if (e.target === e.currentTarget) closeVacancy(); }}
        >
          {/* spec-14/D9: клавиатурная доступность — Escape закрывает (focus
              внутри диалога после autoFocus), описание связано по id. */}
          <div
            ref={vacancyBoxRef}
            className="modal-box"
            role="dialog"
            aria-modal="true"
            aria-labelledby="vacancy-title"
            aria-describedby="vacancy-desc"
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
              стека, процессов и контекста вакансии, адаптируя глубину под ваш уровень.
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
