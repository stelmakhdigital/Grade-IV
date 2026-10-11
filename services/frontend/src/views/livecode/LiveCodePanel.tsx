/**
 * Панель Live-Code (WP-9): редактор + «Запустить тесты» (POST /runs),
 * вывод (тесты со статусами, stdout/stderr), ИИ-ревью.
 * Редактор инжинирится (prop `editor`), чтобы логику можно было
 * тестировать в jsdom без Monaco.
 */
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { ApiError, runTests, type RunResult, type Stack } from '../../api';
import { apiErrorMessage } from '../LoginView';
import { CodeEditor } from './CodeEditor';

export const MAIN_FILE: Record<Stack, string> = {
  go: 'main.go',
  python: 'main.py',
};

export const DEFAULT_CODE: Record<Stack, string> = {
  go: `package main

import "fmt"

func main() {
	// ваше решение
	fmt.Println("Привет, Грейд!")
}
`,
  python: `def main():
    # ваше решение
    print("Привет, Грейд!")


if __name__ == "__main__":
    main()
`,
};

export const LANGUAGE_BY_STACK: Record<Stack, string> = {
  go: 'go',
  python: 'python',
};

/**
 * Код кандидата + результат последнего запуска в рамках SPA. Переживает смену
 * стадий (перезаход в livecode): без этого панель размонтируется при уходе на
 * design и заново инициализируется шаблоном задачи, стирая правки. Ключ кодирует
 * сессию + стек + набор файлов задачи: повторный вход в ту же задачу восстанавливает
 * код, другая задача/стек — нет (своё ключевое пространство, без чужого кода).
 */
interface CodeCacheEntry {
  files: Record<string, string>;
  result: RunResult | null;
}
const codeCache = new Map<string, CodeCacheEntry>();

function cacheKeyFor(
  sessionId: number,
  stack: Stack,
  taskFiles?: Record<string, string> | null,
): string {
  const keySet =
    taskFiles !== undefined && taskFiles !== null && Object.keys(taskFiles).length > 0
      ? Object.keys(taskFiles)
          .sort()
          .join('\u0000')
      : 'default';
  return `${sessionId}|${stack}|${keySet}`;
}

export interface LiveCodePanelProps {
  sessionId: number;
  stack: Stack;
  /** Задача стадии (stage.task) — id для /runs и формулировка. */
  taskId?: string;
  taskTitle?: string;
  /** Файлы задачи из банка (stage.task.files); без них — шаблон main.go/main.py. */
  taskFiles?: Record<string, string> | null;
  /** Реализация редактора (по умолчанию — Monaco). */
  editor?: (props: {
    language: string;
    value: string;
    onChange: (v: string) => void;
  }) => ReactNode;
  /** ИИ-ревью (последний ai_text после запуска) — из WS. */
  review: string;
}

/** Решение кандидата — файл-«solution», если он есть в задаче, иначе main. */
export function solutionFile(stack: Stack, files?: Record<string, string> | null): string {
  if (files !== undefined && files !== null) {
    const sol = Object.keys(files).find((n) => n.startsWith('solution') && n.endsWith('.go') || (n.startsWith('solution') && n.endsWith('.py')));
    if (sol !== undefined) return sol;
    const main = Object.keys(files).find((n) => n === MAIN_FILE[stack]);
    if (main !== undefined) return main;
    return Object.keys(files)[0];
  }
  return MAIN_FILE[stack];
}

export function LiveCodePanel({
  sessionId,
  stack,
  taskId,
  taskTitle,
  taskFiles,
  editor,
  review,
}: LiveCodePanelProps) {
  // Код кандидата + результат последнего запуска восстанавливаем из SPA-кэша при
  // перезаходе (смена стадий); иначе — файлы задачи/шаблон.
  const cacheKey = cacheKeyFor(sessionId, stack, taskFiles);
  const cached = useRef(codeCache.get(cacheKeyFor(sessionId, stack, taskFiles))).current;
  const restored = cached !== undefined && Object.keys(cached.files).length > 0;
  const hasTaskFiles =
    taskFiles !== undefined && taskFiles !== null && Object.keys(taskFiles).length > 0;
  const initialFiles: Record<string, string> = restored
    ? cached.files
    : hasTaskFiles
      ? (taskFiles as Record<string, string>)
      : { [MAIN_FILE[stack]]: DEFAULT_CODE[stack] };
  const [files, setFiles] = useState<Record<string, string>>(initialFiles);
  const [activeFile, setActiveFile] = useState<string>(() => solutionFile(stack, initialFiles));

  // stage.task может прийти по WS после монтирования панели (REST уже дал стадию)
  // — синхронизируем файлы задачи, НЕ затирая восстановленный код кандидата.
  useEffect(() => {
    if (restored) return; // восстановили код кандидата из кэша — не затираем
    if (hasTaskFiles) {
      setFiles(taskFiles as Record<string, string>);
      setActiveFile(solutionFile(stack, taskFiles));
    }
  }, [taskFiles, stack, restored, hasTaskFiles]);
  const [running, setRunning] = useState(false);
  const [result, setResult] = useState<RunResult | null>(() => (restored ? cached.result : null));
  const [error, setError] = useState<string | null>(null);

  // Сохраняем код + результат в кэш (текущий ключ): переживает смену стадий.
  useEffect(() => {
    codeCache.set(cacheKey, { files, result });
  }, [cacheKey, files, result]);

  const onRun = useCallback(async () => {
    if (running) return;
    setRunning(true);
    setError(null);
    try {
      const r = await runTests(sessionId, files, taskId);
      setResult(r);
    } catch (e) {
      if (e instanceof ApiError && (e.code === 'sandbox_error' || e.code === 'sandbox_unavailable')) {
        setError(`Sandbox: ${e.message}`);
      } else {
        setError(apiErrorMessage(e));
      }
    } finally {
      setRunning(false);
    }
  }, [running, sessionId, files, taskId]);

  const Editor = editor ?? defaultEditor;

  const testStats = useMemo(() => {
    if (result === null) return null;
    const passed = result.tests.filter((t) => t.passed).length;
    return { passed, total: result.tests.length };
  }, [result]);

  return (
    <div className="livecode" data-testid="livecode">
      {taskTitle !== undefined && taskTitle !== '' && (
        <p className="task-title" data-testid="task-title">
          {taskTitle}
        </p>
      )}

      <div className="file-tabs" role="tablist">
        {Object.keys(files).map((name) => (
          <button
            key={name}
            type="button"
            role="tab"
            aria-selected={name === activeFile}
            className={name === activeFile ? 'tab active' : 'tab'}
            onClick={() => setActiveFile(name)}
          >
            {name}
          </button>
        ))}
      </div>

      <Editor
        language={LANGUAGE_BY_STACK[stack]}
        value={files[activeFile] ?? ''}
        onChange={(v) => setFiles((prev) => ({ ...prev, [activeFile]: v }))}
      />

      <div className="voice-controls">
        <button
          type="button"
          className="btn primary"
          onClick={() => void onRun()}
          disabled={running}
          data-testid="run-tests"
        >
          {running ? 'Выполняется…' : 'Запустить тесты'}
        </button>
        {testStats !== null && (
          <span className={result !== null && result.passed ? 'run-ok' : 'run-fail'}>
            Тесты: {testStats.passed}/{testStats.total}
            {result !== null && ` · ${result.duration_ms} мс`}
          </span>
        )}
      </div>

      {error !== null && (
        <p className="form-error" role="alert" data-testid="run-error">
          {error}
        </p>
      )}

      {result !== null && (
        <div className="run-output" data-testid="run-output">
          {result.tests.length > 0 && (
            <ul className="test-list">
              {result.tests.map((t) => (
                <li key={t.name} className={t.passed ? 'test-pass' : 'test-fail'}>
                  {t.passed ? '✓' : '✗'} {t.name}
                  {t.output !== undefined && t.output !== '' && (
                    <pre className="test-output">{t.output}</pre>
                  )}
                </li>
              ))}
            </ul>
          )}
          {(result.stdout !== '' || result.stderr !== '') && (
            <pre className="run-stdout">{[result.stdout, result.stderr].filter(Boolean).join('\n')}</pre>
          )}
        </div>
      )}

      {review !== '' && (
        <div className="ai-review" data-testid="ai-review">
          <strong>ИИ-ревью</strong>
          <p>{review}</p>
        </div>
      )}
    </div>
  );
}

function defaultEditor(props: {
  language: string;
  value: string;
  onChange: (v: string) => void;
}): ReactNode {
  return <CodeEditor {...props} />;
}
