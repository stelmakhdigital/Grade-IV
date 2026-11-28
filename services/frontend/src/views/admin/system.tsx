/**
 * Вкладка «Система»: что подключено (LLM, голос, sandbox) и какие модели
 * выбрать. Значения показываются с источником — «сохранил, а ничего не
 * поменялось» почти всегда значит «значение закреплено переменной окружения»,
 * и это должно быть видно до сохранения, а не после (фидбек 2026-11-28).
 *
 * Ничего не применяется на ходу: модель живёт в памяти процесса voice (8 GiB
 * потолок на контейнер, вторая модель рядом не влезает). Честный ответ —
 * «сохранено, будет после перезапуска».
 */
import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  adminPutModels,
  adminRuntime,
  type ModelOption,
  type ModelsPutResult,
  type RuntimeView,
  type SettingSource,
} from '../../api';
import { errText } from './tabs';

const SOURCE_LABEL: Record<SettingSource['source'], string> = {
  env: 'переменная',
  file: 'файл',
  default: 'по умолчанию',
};

/** Откуда значение — короткая подпись у поля. */
function SourceChip({ s }: { s?: SettingSource }) {
  if (!s) return null;
  return (
    <span className={`src-chip ${s.source}`} title={`Значение: ${s.source === 'env' ? `переменная ${s.env}` : SOURCE_LABEL[s.source]}`}>
      {SOURCE_LABEL[s.source]}
      {s.source === 'env' ? ` · ${s.env}` : ''}
    </span>
  );
}

function Row({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div className="runtime-row">
      <span className="runtime-label">{label}</span>
      <div className="runtime-control">{children}</div>
      {hint && <span className="runtime-hint">{hint}</span>}
    </div>
  );
}

/** Модель + «нет весов»: выбор недоступной модели = молчащий интервьюер. */
function modelLabel(m: ModelOption): string {
  return m.present ? m.name : `${m.name} — нет весов`;
}

export function SystemTab() {
  const [view, setView] = useState<RuntimeView | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const [busy, setBusy] = useState<'llm' | 'voice' | null>(null);

  // Форма — только то, что реально можно поменять из панели.
  const [llm, setLlm] = useState({ base_url: '', model: '', enable_thinking: false });
  const [stt, setStt] = useState({ provider: '', model: '' });
  const [tts, setTts] = useState({ provider: '', speaker: '' });

  const load = useCallback(async () => {
    try {
      const v = await adminRuntime();
      setView(v);
      setError(null);
      setLlm({
        base_url: String(v.llm.base_url.value ?? ''),
        model: String(v.llm.model.value ?? ''),
        enable_thinking: v.llm.enable_thinking.value === true,
      });
      const sttProvider = v.voice.current?.stt?.provider ?? '';
      setStt({ provider: sttProvider, model: v.voice.current?.stt?.model ?? '' });
      setTts({
        provider: v.voice.current?.tts?.provider ?? '',
        speaker: v.voice.current?.tts?.loaded ? (v.voice.current.tts.speakers?.[0] ?? '') : '',
      });
    } catch (e) {
      setError(errText(e));
    }
  }, []);
  useEffect(() => {
    void load();
  }, [load]);

  const sttProviders = useMemo(() => Object.keys(view?.voice.catalog?.stt ?? {}), [view]);
  const ttsProviders = useMemo(() => Object.keys(view?.voice.catalog?.tts ?? {}), [view]);
  const sttModels = (view?.voice.catalog?.stt?.[stt.provider] ?? []) as ModelOption[];
  const selectedStt = sttModels.find((m) => m.name === stt.model);
  const speakers = view?.voice.current?.tts?.speakers ?? [];

  const report = (r: ModelsPutResult) => {
    const parts: string[] = [];
    for (const [k, v] of Object.entries(r.applied)) {
      if (v.saved) parts.push(`${k}: сохранено`);
      else if (v.error) parts.push(`${k}: ${v.error}`);
      else if (v.status && v.status >= 400) parts.push(`${k}: отклонено (${v.status})`);
    }
    const missing = (r.applied.voice as { body?: { missing_weights?: Record<string, string[]> } })
      ?.body?.missing_weights;
    const gaps = missing ? Object.keys(missing) : [];
    setNote(
      (parts.join(' · ') || 'изменений нет') +
        ' — применится после перезапуска сервиса' +
        (gaps.length ? `. Весов нет: ${gaps.join(', ')} — интерфейс останется без этой части` : ''),
    );
  };

  const saveLlm = async () => {
    setBusy('llm');
    setError(null);
    setNote(null);
    try {
      report(await adminPutModels({ llm }));
      await load();
    } catch (e) {
      setError(errText(e));
    } finally {
      setBusy(null);
    }
  };

  const saveVoice = async () => {
    setBusy('voice');
    setError(null);
    setNote(null);
    try {
      report(await adminPutModels({ stt, tts }));
      await load();
    } catch (e) {
      setError(errText(e));
    } finally {
      setBusy(null);
    }
  };

  if (!view) {
    return error ? <p className="error" role="alert">{error}</p> : <p className="muted">Загружаем…</p>;
  }

  const voiceDown = Boolean(view.voice.error);
  // Поле «закреплено переменной» не редактируется файлом: показываем его как есть
  // и объясняем, где менять, иначе правка выглядит потерянной.
  const pinned = (s?: SettingSource) => s?.source === 'env';

  return (
    <section className="card runtime">
      <div className="history-head">
        <h2>Система</h2>
        <span className="runtime-env" title="APP_ENV">
          {view.app_env}
        </span>
      </div>
      <p className="muted runtime-note">{view.applies_note}</p>

      {error && <p className="error" role="alert">{error}</p>}
      {note && <p className="runtime-note-ok" role="status">{note}</p>}
      {voiceDown && (
        <p className="error" role="alert">
          Голосовой сервис не отвечает: {view.voice.error}. Проверьте VOICE_URL и что контейнер
          voice поднят.
        </p>
      )}

      <h3>Язык модели (LLM)</h3>
      {view.llm.mock && (
        <p className="error" role="alert">
          Включён LLM_MOCK — собеседование ведёт заглушка, ответы заготовлены.
        </p>
      )}
      <Row label="Эндпоинт" hint={view.llm.config_path}>
        <input
          className="input"
          value={llm.base_url}
          disabled={pinned(view.llm.base_url)}
          onChange={(e) => setLlm({ ...llm, base_url: e.target.value })}
          aria-label="Эндпоинт LLM"
        />
        <SourceChip s={view.llm.base_url} />
      </Row>
      <Row
        label="Модель"
        hint={
          pinned(view.llm.model)
            ? 'Закреплено переменной — файл её не перебивает'
            : 'Пусто — определим по /models при старте'
        }
      >
        <input
          className="input"
          value={llm.model}
          disabled={pinned(view.llm.model)}
          onChange={(e) => setLlm({ ...llm, model: e.target.value })}
          aria-label="Модель LLM"
        />
        <SourceChip s={view.llm.model} />
      </Row>
      <Row label="Размышление модели" hint="Для моделей с режимом thinking (Qwen3 и др.)">
        <label className="runtime-check">
          <input
            type="checkbox"
            checked={llm.enable_thinking}
            disabled={pinned(view.llm.enable_thinking)}
            onChange={(e) => setLlm({ ...llm, enable_thinking: e.target.checked })}
          />
          включено
        </label>
        <SourceChip s={view.llm.enable_thinking} />
      </Row>
      <Row label="Ключ доступа">
        <span className={view.llm.api_key_set ? 'runtime-ok' : 'runtime-bad'}>
          {view.llm.api_key_set ? 'задан' : 'не задан'}
        </span>
        <span className="runtime-hint">
          Ключ — только переменная окружения (LLM_API_KEY), в настройках его нет
        </span>
      </Row>
      <div className="runtime-actions">
        <button type="button" className="btn primary small" onClick={saveLlm} disabled={busy !== null}>
          {busy === 'llm' ? 'Сохраняем…' : 'Сохранить LLM'}
        </button>
      </div>

      <h3>Голос</h3>
      {view.voice.path && (
        <p className="muted runtime-hint-file">
          Файл настроек: <code>{view.voice.path}</code>
        </p>
      )}
      <Row label="Распознавание (STT)" hint="GigaAM — дефолт проекта; Whisper включается осознанно">
        <select
          className="select"
          value={stt.provider}
          onChange={(e) => {
            const p = e.target.value;
            const first = (view.voice.catalog?.stt?.[p] ?? []).find((m) => m.present) ??
              (view.voice.catalog?.stt?.[p] ?? [])[0];
            setStt({ provider: p, model: first?.name ?? '' });
          }}
          aria-label="Провайдер распознавания"
        >
          {sttProviders.map((p) => (
            <option key={p} value={p}>
              {p}
            </option>
          ))}
        </select>
        <SourceChip s={view.voice.sources?.stt?.provider} />
      </Row>
      <Row
        label="Модель распознавания"
        hint={
          selectedStt && !selectedStt.present
            ? `Нет весов: ${selectedStt.missing.join(', ')}. Положите файлы в каталог моделей и перезапустите voice.`
            : ''
        }
      >
        <select
          className="select"
          value={stt.model}
          onChange={(e) => setStt({ ...stt, model: e.target.value })}
          aria-label="Модель распознавания"
        >
          {sttModels.map((m) => (
            <option key={m.name} value={m.name}>
              {modelLabel(m)}
            </option>
          ))}
        </select>
        <SourceChip s={view.voice.sources?.stt?.model?.[stt.provider]} />
      </Row>
      <Row label="Синтез (TTS)">
        <select
          className="select"
          value={tts.provider}
          onChange={(e) => setTts({ ...tts, provider: e.target.value })}
          aria-label="Провайдер синтеза"
        >
          {ttsProviders.map((p) => (
            <option key={p} value={p}>
              {p}
            </option>
          ))}
        </select>
        <SourceChip s={view.voice.sources?.tts?.provider} />
      </Row>
      <Row label="Голос интервьюера" hint={speakers.length ? '' : 'Голоса появятся, когда модель синтеза загружена'}>
        <select
          className="select"
          value={tts.speaker}
          onChange={(e) => setTts({ ...tts, speaker: e.target.value })}
          disabled={!speakers.length}
          aria-label="Голос интервьюера"
        >
          {(speakers.length ? speakers : [tts.speaker].filter(Boolean)).map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
        <SourceChip s={view.voice.sources?.tts?.speaker} />
      </Row>
      <div className="runtime-actions">
        <button
          type="button"
          className="btn primary small"
          onClick={saveVoice}
          disabled={busy !== null || voiceDown || view.voice.writable === false}
        >
          {busy === 'voice' ? 'Сохраняем…' : 'Сохранить голос'}
        </button>
        {view.voice.writable === false && (
          <span className="runtime-hint">
            Запись выключена: у voice не задан VOICE_ADMIN_TOKEN
          </span>
        )}
      </div>

      <h3>Песочница кода</h3>
      <Row label="Адрес">
        <span className="runtime-static">{view.sandbox.url}</span>
        <span className={view.sandbox.admin_token_set ? 'runtime-ok' : 'runtime-bad'}>
          {view.sandbox.admin_token_set ? 'управление доступно' : 'нет SANDBOX_ADMIN_TOKEN'}
        </span>
      </Row>
      <Row label="Пополнение банка задач">
        <span className="runtime-static">
            не ниже {view.sandbox.tasks_min} задач, каждые {Math.round(view.sandbox.tasks_refill_s / 60)} мин
        </span>
      </Row>
    </section>
  );
}
