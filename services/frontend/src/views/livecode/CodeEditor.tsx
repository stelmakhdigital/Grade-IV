/**
 * Monaco-редактор (WP-9). Динамическая загрузка — бандл кабинета
 * остаётся лёгким; в jsdom-тестах компонент не используется
  (LiveCodePanel тестируется с инъектированным редактором).
 */
import { Editor, loader, type OnMount } from '@monaco-editor/react';
import { useId } from 'react';

// Self-hosted Monaco (spec-15): loader по умолчанию тянет monaco с CDN
// (jsdelivr) — оффлайн/LAN-интервью без интернета оставят «Загрузка
// редактора…» навсегда. Пакет monaco-editor уже в зависимостях; его min/vs
// скопирован в public/monaco/vs (vite отдаёт из public и в dev, и в build).
loader.config({ paths: { vs: '/monaco/vs' } });

export interface CodeEditorProps {
  language: string;
  value: string;
  onChange: (v: string) => void;
  onReady?: () => void;
}

export function CodeEditor({ language, value, onChange, onReady }: CodeEditorProps) {
  const label = useId();
  const handleMount: OnMount = () => {
    onReady?.();
  };
  return (
    <Editor
      key={label}
      height="320px"
      language={language}
      value={value}
      // Тема — через ПРОП theme (не options): обёртка после create явно
      // вызывает setTheme(themeProp), а проп без передачи = "light" —
      // раньше редактор вспыхивал светлой темой в тёмном кабинете.
      theme="vs-dark"
      onChange={(v) => onChange(v ?? '')}
      onMount={handleMount}
      loading={
        <div className="code-loading" role="status" aria-label="Загрузка редактора">
          <span className="code-loading-spinner" aria-hidden />
          Загрузка редактора…
        </div>
      }
      options={{
        fontSize: 14,
        minimap: { enabled: false },
        automaticLayout: true,
        scrollBeyondLastLine: false,
        tabSize: 4,
      }}
    />
  );
}
