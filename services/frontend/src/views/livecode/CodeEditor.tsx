/**
 * Monaco-редактор (WP-9). Динамическая загрузка — бандл кабинета
 * остаётся лёгким; в jsdom-тестах компонент не используется
  (LiveCodePanel тестируется с инъектированным редактором).
 */
import { Editor, type OnMount } from '@monaco-editor/react';
import { useId } from 'react';

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
      onChange={(v) => onChange(v ?? '')}
      onMount={handleMount}
      loading={<div className="muted">Загрузка редактора…</div>}
      options={{
        fontSize: 14,
        minimap: { enabled: false },
        automaticLayout: true,
        scrollBeyondLastLine: false,
        tabSize: 4,
        theme: 'vs-dark',
      }}
    />
  );
}
