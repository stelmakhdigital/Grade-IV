/**
 * Настройки (`#/settings`) — одна поверхность для всего, что настраивается.
 *
 * Разделение не по «человек / стенд», а по состоянию применения (ADR-015):
 *  - сразу: тема, голос и характер интервьюера (localStorage этого браузера);
 *  - перезапуск: модель и эндпоинт LLM, адрес voice, модели STT/TTS и спикер,
 *    адрес песочницы, пороги тишины/VAD, рамки сессии (config/app.json, голос —
 *    через voice);
 *  - только чтение: адрес БД, порт, уровень логов, каталог резюме, факты
 *    наличия секретов. Их правят в .env: ошибка лишает приложения без «отменить».
 *
 * Каждое редактируемое значение подписано источником (переменная | файл | по
 * умолчанию), а сверху баннер из `pending`: «ждёт перезапуска» или «правка
 * мертва, значение закреплено переменной». Различать эти два случая глазами по
 * сохранённой форме — главная причина, почему настройки раньше казались сломанными.
 *
 * Секций-редакторов (шаблоны, начала разговора, реестр голосов, роли) здесь нет:
 * они живут во вкладках админки и переедут сюда отдельно — это перенос форм, а
 * не контракта.
 */
import { useCallback, useEffect, useState } from 'react';
import {
  getSettings,
  listProfiles,
  listTTSVoices,
  putSettings,
  type ModelOption,
  type PendingItem,
  type SettingsPut,
  type SettingsView as Settings,
  type SettingSource,
} from '../api';
import { useAuth } from '../auth';
import { prefProfileId, prefSpeaker, setPrefProfileId, setPrefSpeaker } from '../prefs';
import { ThemeToggle } from '../ThemeToggle';
import { currentTheme, storedTheme } from '../theme';
import { errText } from './admin/tabs';

const TONE: Record<string, string> = {
  supportive: 'поддерживающий',
  balanced: 'нейтральный',
  strict: 'строгий',
  playful: 'игривый',
  socratic: 'сократовский',
};
const DIFFICULTY: Record<string, string> = {
  minus: 'мягче',
  standard: 'обычный',
  plus: 'жёстче',
};
/** Что означают пороги — одной строкой, без перехода в документацию. */
const TIMING_LABEL: Record<string, { label: string; hint: string }> = {
  silence_nudge_s: { label: 'Молчание до напоминания', hint: 'секунд тишины, после которых интервьюер подсказывает' },
  vad_end_silence_ms: { label: 'Конец реплики (мс)', hint: 'пауза, по которой речь считается законченной' },
  vad_rms_threshold: { label: 'Порог шума', hint: 'громкость ниже порога считается тишиной' },
  vad_prestt_silence_ms: { label: 'Отступ перед распознаванием (мс)', hint: 'сколько тишины добавить к началу аудио' },
  session_pause_timeout_s: { label: 'Пауза до срыва (сек)', hint: 'дольше — интервью прерывается' },
  session_limit_s: { label: 'Лимит интервью (сек)', hint: '0 — по грейду (45/50/60/75 мин)' },
};

function Chip({ s }: { s?: SettingSource }) {
  if (!s) return null;
  const label = s.source === 'env' ? `переменная · ${s.env}` : s.source === 'file' ? 'файл' : 'по умолчанию';
  return <span className={`src-chip ${s.source}`}>{label}</span>;
}

function Row({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div className="settings-line">
      <span className="settings-key">{label}</span>
      <div className="settings-control">{children}</div>
      {hint && <span className="settings-note">{hint}</span>}
    </div>
  );
}

/** Баннер того, что файл уже просит, а сервис ещё не применил. */
function RestartBanner({ pending }: { pending: PendingItem[] }) {
  if (!pending.length) return null;
  const restart = pending.filter((p) => p.status === 'needs_restart');
  const shadow = pending.filter((p) => p.status === 'shadowed_by_env');
  return (
    <p className="settings-pending" role="status">
      {restart.length > 0 && (
        <>
          {restart.length} изменени{restart.length === 1 ? 'е' : 'й'} ждут перезапуска: {restart.map((p) => p.key).join(', ')}.
        </>
      )}
      {shadow.length > 0 && (
        <>
          {' '}
          Закреплено переменными и не читается из файла: {shadow.map((p) => `${p.key} (${p.env})`).join(', ')}.
        </>
      )}
      {' '}
      Перезапуск: <code>bash scripts/run-all.sh stop</code> и повторный запуск (прод —{' '}
      <code>docker compose restart</code>).
    </p>
  );
}

const s = (v: SettingSource | undefined, fallback = ''): string => {
  const raw = v?.value;
  return raw === null || raw === undefined ? fallback : String(raw);
};

export function SettingsView() {
  const { user } = useAuth();
  const admin = user?.is_admin === true;
  const [view, setView] = useState<Settings | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [theme, setTheme] = useState(currentTheme());
  const [manual, setManual] = useState(storedTheme() !== null);

  const [voices, setVoices] = useState<{ id: string; name: string; speaker: string }[]>([]);
  const [profiles, setProfiles] = useState<{ id: number; name: string; tone: string; difficulty: string }[]>([]);
  const [speaker, setSpeaker] = useState(prefSpeaker());
  const [profileId, setProfileId] = useState<number | null>(prefProfileId());

  const [llm, setLlm] = useState({ base_url: '', model: '', enable_thinking: false });
  const [voiceUrl, setVoiceUrl] = useState('');
  const [sandboxUrl, setSandboxUrl] = useState('');
  const [stt, setStt] = useState({ provider: '', model: '' });
  const [tts, setTts] = useState({ provider: '', speaker: '' });
  const [timing, setTiming] = useState<Record<string, string>>({});

  const load = useCallback(async () => {
    const [v, p] = await Promise.allSettled([listTTSVoices(), listProfiles()]);
    if (v.status === 'fulfilled') {
      setVoices(v.value.voices ?? []);
      const saved = prefSpeaker();
      setSpeaker((v.value.voices ?? []).some((x) => x.speaker === saved) ? saved : (v.value.default ?? ''));
    }
    if (p.status === 'fulfilled') {
      setProfiles(p.value);
      const saved = prefProfileId();
      setProfileId(p.value.some((x) => x.id === saved) ? saved : (p.value[0]?.id ?? null));
    }
    if (!admin) return;
    try {
      const st = await getSettings();
      setView(st);
      setLlm({ base_url: s(st.llm.base_url), model: s(st.llm.model), enable_thinking: st.llm.enable_thinking.value === true });
      setVoiceUrl(s(st.voice.address));
      setSandboxUrl(s(st.sandbox.url));
      setStt({ provider: st.voice.current?.stt?.provider ?? '', model: st.voice.current?.stt?.model ?? '' });
      setTts({ provider: st.voice.current?.tts?.provider ?? '', speaker: st.voice.current?.tts?.loaded ? (st.voice.current.tts.speakers?.[0] ?? '') : '' });
      setTiming(Object.fromEntries(Object.entries(st.timing).map(([k, v]) => [k, s(v, '0')])));
    } catch (e) {
      setError(errText(e));
    }
  }, [admin]);
  useEffect(() => {
    void load();
  }, [load]);

  const save = async (key: string, payload: SettingsPut) => {
    setBusy(key);
    setError(null);
    setNote(null);
    try {
      const r = await putSettings(payload);
      const parts = Object.entries(r.applied).map(([k, v]) =>
        v.saved ? `${k}: сохранено` : v.error ? `${k}: ${v.error}` : `${k}: отклонено (${v.status})`,
      );
      setNote(`${parts.join(' · ')} — применится после перезапуска сервиса`);
      const st = await getSettings();
      setView(st);
    } catch (e) {
      setError(errText(e));
    } finally {
      setBusy(null);
    }
  };

  if (!admin) {
    return (
      <main className="app cabinet settings">
        <header className="header">
          <div>
            <h1>Настройки</h1>
            <p className="muted small">{user?.email}</p>
          </div>
          <a className="btn ghost" href="#/">В кабинет</a>
        </header>
        <section className="card" aria-labelledby="set-appearance">
          <h2 id="set-appearance">Внешний вид</h2>
          <Row label="Тема" hint="Применяется сразу и хранится в этом браузере. «Авто» следует настройке системы.">
            <ThemeToggle onChange={(t) => { setTheme(t); setManual(storedTheme() !== null); }} />
          </Row>
          <p className="muted small">Сейчас {theme === 'light' ? 'светлая' : 'тёмная'} — {manual ? 'выбрано вручную' : 'как в системе'}.</p>
        </section>
        <section className="card" aria-labelledby="set-interviewer">
          <h2 id="set-interviewer">Интервьюер</h2>
          <Row label="Голос">
            <select className="select" value={speaker} aria-label="Голос интервьюера"
              onChange={(e) => { setSpeaker(e.target.value); setPrefSpeaker(e.target.value); }}>
              {voices.map((v) => (<option key={v.id} value={v.speaker}>{v.name}</option>))}
            </select>
          </Row>
          <Row label="Характер">
            <select className="select" value={profileId ?? ''} aria-label="Профиль интервьюера"
              onChange={(e) => { const id = Number(e.target.value); setProfileId(id); setPrefProfileId(id); }}>
              {profiles.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name} — {TONE[p.tone] ?? p.tone}, {DIFFICULTY[p.difficulty] ?? p.difficulty}
                </option>
              ))}
            </select>
          </Row>
          <p className="muted small">
            Выбор по умолчанию: перед стартом он стоит в форме «Новое интервью» и там меняется на одно интервью.
          </p>
        </section>
      </main>
    );
  }

  if (view === null) {
    return (
      <main className="app cabinet settings">
        <h1>Настройки</h1>
        {error ? <p className="error" role="alert">{error}</p> : <p className="muted">Загружаем…</p>}
      </main>
    );
  }

  const sttProviders = Object.keys(view.voice.catalog?.stt ?? {});
  const sttModels: ModelOption[] = view.voice.catalog?.stt?.[stt.provider] ?? [];
  const selected = sttModels.find((m) => m.name === stt.model);
  const voiceDown = Boolean(view.voice.error);

  return (
    <main className="app cabinet settings">
      <header className="header">
        <div>
          <h1>Настройки</h1>
          <p className="muted small">{user?.email} · {view.app_env}</p>
        </div>
        <a className="btn ghost" href="#/">В кабинет</a>
      </header>

      <RestartBanner pending={view.pending ?? []} />
      {error && <p className="error" role="alert">{error}</p>}
      {note && <p className="runtime-note-ok" role="status">{note}</p>}

      <section className="card" aria-labelledby="set-appearance">
        <h2 id="set-appearance">Внешний вид</h2>
        <Row label="Тема">
          <ThemeToggle onChange={(t) => { setTheme(t); setManual(storedTheme() !== null); }} />
          <span className="settings-note">
            Сейчас {theme === 'light' ? 'светлая' : 'тёмная'} — {manual ? 'выбрано вручную' : 'как в системе'}
          </span>
        </Row>
      </section>

      <section className="card" aria-labelledby="set-interviewer">
        <h2 id="set-interviewer">Интервьюер</h2>
        <Row label="Голос">
          <select className="select" value={speaker} aria-label="Голос интервьюера"
            onChange={(e) => { setSpeaker(e.target.value); setPrefSpeaker(e.target.value); }}>
            {voices.map((v) => (<option key={v.id} value={v.speaker}>{v.name}</option>))}
          </select>
        </Row>
        <Row label="Характер">
          <select className="select" value={profileId ?? ''} aria-label="Профиль интервьюера"
            onChange={(e) => { const id = Number(e.target.value); setProfileId(id); setPrefProfileId(id); }}>
            {profiles.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name} — {TONE[p.tone] ?? p.tone}, {DIFFICULTY[p.difficulty] ?? p.difficulty}
              </option>
            ))}
          </select>
        </Row>
        <p className="muted small">
          Выбор по умолчанию: перед стартом он стоит в форме «Новое интервью» и там меняется на одно интервью.
        </p>
      </section>

      <section className="card" aria-labelledby="set-llm">
        <h2 id="set-llm">Язык модели (LLM)</h2>
        {view.llm.mock && <p className="error" role="alert">Включён LLM_MOCK — собеседование ведёт заглушка.</p>}
        <Row label="Эндпоинт" hint={view.llm.config_path}>
          <input className="input" value={llm.base_url} aria-label="Эндпоинт LLM"
            onChange={(e) => setLlm({ ...llm, base_url: e.target.value })} />
          <Chip s={view.llm.base_url} />
        </Row>
        <Row label="Модель" hint="Пусто — определим по /models при старте">
          <input className="input" value={llm.model} aria-label="Модель LLM"
            onChange={(e) => setLlm({ ...llm, model: e.target.value })} />
          <Chip s={view.llm.model} />
        </Row>
        <Row label="Размышление модели" hint="Для моделей с режимом thinking (Qwen3 и др.)">
          <label className="runtime-check">
            <input type="checkbox" checked={llm.enable_thinking}
              onChange={(e) => setLlm({ ...llm, enable_thinking: e.target.checked })} />
            включено
          </label>
          <Chip s={view.llm.enable_thinking} />
        </Row>
        <Row label="Ключ доступа">
          <span className={view.llm.api_key_set ? 'runtime-ok' : 'runtime-bad'}>
            {view.llm.api_key_set ? 'задан' : 'не задан'}
          </span>
          <span className="settings-note">Ключ — только переменная окружения, в настройки он не приезжает</span>
        </Row>
        <div className="runtime-actions">
          <button type="button" className="btn primary small" disabled={busy !== null}
            onClick={() => save('llm', { llm })}>
            {busy === 'llm' ? 'Сохраняем…' : 'Сохранить LLM'}
          </button>
        </div>
      </section>

      <section className="card" aria-labelledby="set-speech">
        <h2 id="set-speech">Речь и распознавание</h2>
        {voiceDown && <p className="error" role="alert">Голосовой сервис не отвечает: {view.voice.error}</p>}
        <Row label="Адрес голосового сервиса" hint={view.paths?.voice_file ? `файл голоса: ${view.paths.voice_file}` : ''}>
          <input className="input" value={voiceUrl} aria-label="Адрес voice"
            onChange={(e) => setVoiceUrl(e.target.value)} />
          <Chip s={view.voice.address} />
        </Row>
        <Row label="Распознавание (STT)" hint="GigaAM — дефолт проекта; Whisper включается осознанно">
          <select className="select" value={stt.provider} aria-label="Провайдер распознавания"
            onChange={(e) => {
              const p = e.target.value;
              const first = (view.voice.catalog?.stt?.[p] ?? []).find((m) => m.present) ??
                (view.voice.catalog?.stt?.[p] ?? [])[0];
              setStt({ provider: p, model: first?.name ?? '' });
            }}>
            {sttProviders.map((p) => (<option key={p} value={p}>{p}</option>))}
          </select>
        </Row>
        <Row label="Модель распознавания" hint={selected && !selected.present
          ? `Нет весов: ${selected.missing.join(', ')}. Положите файлы в каталог моделей и перезапустите voice.`
          : ''}>
          <select className="select" value={stt.model} aria-label="Модель распознавания"
            onChange={(e) => setStt({ ...stt, model: e.target.value })}>
            {sttModels.map((m) => (<option key={m.name} value={m.name}>{m.present ? m.name : `${m.name} — нет весов`}</option>))}
          </select>
        </Row>
        <Row label="Голос синтеза" hint="Список даёт загруженная модель синтеза">
          <select className="select" value={tts.speaker} aria-label="Спикер синтеза"
            disabled={!(view.voice.current?.tts?.speakers?.length ?? 0)}
            onChange={(e) => setTts({ provider: tts.provider, speaker: e.target.value })}>
            {(view.voice.current?.tts?.speakers ?? []).map((x) => (<option key={x} value={x}>{x}</option>))}
          </select>
        </Row>
        <div className="runtime-actions">
          <button type="button" className="btn primary small" disabled={busy !== null || voiceDown}
            onClick={() => save('speech', {
              ...(voiceUrl ? { voice_url: voiceUrl } : {}),
              stt,
              tts,
            })}>
            {busy === 'speech' ? 'Сохраняем…' : 'Сохранить речь'}
          </button>
          {!view.writable && <span className="settings-note">Запись голоса выключена: у voice не задан VOICE_ADMIN_TOKEN</span>}
        </div>
      </section>

      <section className="card" aria-labelledby="set-timing">
        <h2 id="set-timing">Тайминги и пороги</h2>
        {Object.entries(TIMING_LABEL).map(([key, meta]) => (
          <Row key={key} label={meta.label} hint={meta.hint}>
            <input className="input" inputMode="numeric" value={timing[key] ?? ''} aria-label={meta.label}
              onChange={(e) => setTiming({ ...timing, [key]: e.target.value.replace(/[^\d]/g, '') })} />
            <Chip s={view.timing[key]} />
          </Row>
        ))}
        <div className="runtime-actions">
          <button type="button" className="btn primary small" disabled={busy !== null}
            onClick={() => save('timing', { timing: Object.fromEntries(
              Object.entries(timing).filter(([, v]) => v !== '').map(([k, v]) => [k, Number(v)]),
            ) })}>
            {busy === 'timing' ? 'Сохраняем…' : 'Сохранить тайминги'}
          </button>
        </div>
      </section>

      <section className="card" aria-labelledby="set-stand">
        <h2 id="set-stand">Песочница и установка</h2>
        <Row label="Адрес песочницы кода" hint="Live-Code: запуск тестов кандидата">
          <input className="input" value={sandboxUrl}
            aria-label="Адрес песочницы" onChange={(e) => setSandboxUrl(e.target.value)} />
          <Chip s={view.sandbox.url} />
        </Row>
        <div className="runtime-actions">
          <button type="button" className="btn primary small" disabled={busy !== null || sandboxUrl === s(view.sandbox.url)}
            onClick={() => save('sandbox', { sandbox_url: sandboxUrl })}>
            {busy === 'sandbox' ? 'Сохраняем…' : 'Сохранить адрес'}
          </button>
          <span className="settings-note">
            {view.sandbox.admin_token_set ? 'Управление банком задач доступно' : 'Нет SANDBOX_ADMIN_TOKEN — банк задач не пополнить'}
          </span>
        </div>
        <h3>Только чтение (переменные окружения)</h3>
        {Object.entries(view.install).filter(([k]) => k !== 'note' && k !== 'editable').map(([k, v]) => (
          <div key={k} className="settings-line">
            <span className="settings-key">{k}</span>
            <span className="settings-value">{typeof v === 'boolean' ? (v ? 'задано' : 'не задано') : String(v)}</span>
          </div>
        ))}
        <p className="muted small">
          {String(view.install.note ?? '')} Файлы: {view.paths?.app_file}
          {view.paths?.voice_file ? `, ${view.paths.voice_file}` : ''}
        </p>
      </section>
    </main>
  );
}
