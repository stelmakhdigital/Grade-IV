/**
 * Настройки (экран `#/settings`).
 *
 * Где что настраивается — разделение по двум осям: чьё это решение и когдаоно
 * применяется.
 *
 *  Здесь — решения человека, которые действуют всегда и применяются сразу:
 *  тема интерфейса, голос и профиль интервьюера (это постоянный выбор, а не
 *  параметр одного интервью — перед стартом его всё равно видно в форме).
 *
 *  В админке — решения стенда: модели (LLM/STT/TTS), эндпоинт, пороги VAD и
 *  тишины, лимиты паузы/сессии, банк задач sandbox, шаблоны, начала разговора,
 *  реестр голосов, роли. Другие права, другая цена ошибки (неверная модель =
 *  молчащий интервьюер) и другой момент применения — с перезапуском сервиса.
 *  Отсюда на них только указатель: один экран, где видно ВСЁ, но правится то,
 *  что человеку под силу.
 *
 *  Чего в интерфейсе нет сознательно: секреты (JWT_SECRET, ключ LLM, пароли БД)
 *  — только переменные окружения; адрес БД, порты, лимиты контейнеров — состав
 *  деплоя (docs/OPERATIONS.md).
 */
import { useCallback, useEffect, useState } from 'react';
import {
  adminRuntime,
  listProfiles,
  listTTSVoices,
  type InterviewProfile,
  type TTSVoiceRegistry,
} from '../api';
import { useAuth } from '../auth';
import { prefProfileId, prefSpeaker, setPrefProfileId, setPrefSpeaker } from '../prefs';
import { ThemeToggle } from '../ThemeToggle';
import { currentTheme, storedTheme } from '../theme';

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

function Line({ label, value, note }: { label: string; value: string; note?: string }) {
  return (
    <div className="settings-line">
      <span className="settings-key">{label}</span>
      <span className="settings-value">{value}</span>
      {note && <span className="settings-note">{note}</span>}
    </div>
  );
}

export function SettingsView() {
  const { user } = useAuth();
  const [voices, setVoices] = useState<TTSVoiceRegistry | null>(null);
  const [profiles, setProfiles] = useState<InterviewProfile[]>([]);
  const [speaker, setSpeaker] = useState<string>(prefSpeaker());
  const [profileId, setProfileId] = useState<number | null>(prefProfileId());
  const [runtime, setRuntime] = useState<Awaited<ReturnType<typeof adminRuntime>> | null>(null);
  // Тема живёт в ThemeToggle (он знает про «Авто» и слушатель системы); здесь
  // только подпись под контролом — обновляем по колбэку переключателя.
  const [theme, setTheme] = useState(currentTheme());
  const [manual, setManual] = useState(storedTheme() !== null);

  const load = useCallback(async () => {
    const [v, p] = await Promise.allSettled([listTTSVoices(), listProfiles()]);
    if (v.status === 'fulfilled') {
      setVoices(v.value);
      // Сохранённый голос мог быть убран из реестра админом — показываем факт.
      const saved = prefSpeaker();
      const known = (v.value.voices ?? []).some((x) => x.speaker === saved);
      setSpeaker(known ? saved : (v.value.default ?? ''));
    }
    if (p.status === 'fulfilled') {
      setProfiles(p.value);
      const saved = prefProfileId();
      setProfileId(p.value.some((x) => x.id === saved) ? saved : (p.value[0]?.id ?? null));
    }
    if (user?.is_admin) {
      // Блок «стенд» read-only и не критичен: не пускаем ошибку в раздел настроек.
      adminRuntime().then(setRuntime).catch(() => setRuntime(null));
    }
  }, [user?.is_admin]);
  useEffect(() => {
    void load();
  }, [load]);

  return (
    <main className="app cabinet settings">
      <header className="header">
        <div>
          <h1>Настройки</h1>
          <p className="muted small">{user?.email}</p>
        </div>
        <a className="btn ghost" href="#/">
          В кабинет
        </a>
      </header>

      <section className="card" aria-labelledby="set-appearance">
        <h2 id="set-appearance">Внешний вид</h2>
        <div className="settings-line">
          <span className="settings-key">Тема</span>
          <ThemeToggle
            onChange={(t) => {
              setTheme(t);
              setManual(storedTheme() !== null);
            }}
          />
          <span className="settings-note">
            Сейчас {theme === 'light' ? 'светлая' : 'тёмная'} —{' '}
            {manual ? 'выбрано вручную' : 'как в системе'}
          </span>
        </div>
        <p className="muted small">
          Применяется сразу, хранится в этом браузере. «Авто» следует настройке
          системы.
        </p>
      </section>

      <section className="card" aria-labelledby="set-interviewer">
        <h2 id="set-interviewer">Интервьюер</h2>
        <div className="settings-line">
          <span className="settings-key">Голос</span>
          <select
            className="select"
            value={speaker}
            onChange={(e) => {
              setSpeaker(e.target.value);
              setPrefSpeaker(e.target.value);
            }}
            aria-label="Голос интервьюера"
          >
            {(voices?.voices ?? []).map((v) => (
              <option key={v.id} value={v.speaker}>
                {v.name}
              </option>
            ))}
          </select>
        </div>
        <div className="settings-line">
          <span className="settings-key">Характер</span>
          <select
            className="select"
            value={profileId ?? ''}
            onChange={(e) => {
              const id = Number(e.target.value);
              setProfileId(id);
              setPrefProfileId(id);
            }}
            aria-label="Профиль интервьюера"
          >
            {profiles.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name} — {TONE[p.tone] ?? p.tone}, {DIFFICULTY[p.difficulty] ?? p.difficulty}
              </option>
            ))}
          </select>
        </div>
        <p className="muted small">
          Это выбор по умолчанию: перед стартом интервью он стоит в форме
          «Новое интервью» и там же меняется на одно интервью.
        </p>
      </section>

      {user?.is_admin && (
        <section className="card" aria-labelledby="set-stand">
          <h2 id="set-stand">Стенд</h2>
          {runtime === null ? (
            <p className="muted small">
              Сводка недоступна: сервисы не ответили. Настройки стенда — в{' '}
              <a href="#/admin?tab=system">админке</a>.
            </p>
          ) : (
            <>
              <Line
                label="Модель (LLM)"
                value={String(runtime.llm.model.value ?? 'авто по /models')}
                note={String(runtime.llm.base_url.value ?? '')}
              />
              <Line
                label="Распознавание"
                value={`${runtime.voice.current?.stt?.provider ?? '—'} · ${runtime.voice.current?.stt?.model ?? ''}`}
                note={runtime.voice.error ? 'voice недоступен' : runtime.voice.current?.stt?.loaded ? 'загружено' : 'не загружено'}
              />
              <Line
                label="Синтез"
                value={`${runtime.voice.current?.tts?.provider ?? '—'} · ${runtime.voice.current?.tts?.model ?? ''}`}
                note={runtime.voice.current?.tts?.loaded ? 'загружено' : 'не загружено'}
              />
              <Line label="Песочница" value={runtime.sandbox.url} />
            </>
          )}
          <p className="muted small">
            Модели, эндпоинт, пороги тишины/VAD, лимиты паузы и банка задач
            правятся в админке и применяются перезапуском сервиса. Ключи —
            только переменные окружения, в интерфейс они не приезжают.
          </p>
          <p className="settings-links">
            <a href="#/admin?tab=system">Модели и параметры стенда</a>
            <a href="#/admin?tab=voices">Реестр голосов</a>
            <a href="#/admin?tab=templates">Шаблоны интервью</a>
            <a href="#/admin?tab=openings">Начала разговора</a>
            <a href="#/admin?tab=tracks">Роли собеседований</a>
          </p>
        </section>
      )}
    </main>
  );
}
