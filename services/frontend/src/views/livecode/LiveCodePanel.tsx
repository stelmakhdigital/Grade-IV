/**
 * Панель Live-Code (WP-9, spec-13B T2): вкладки задач «Задача 1..N»
 * (статус-точка: серая — нет сдач, зелёная — последний run passed, красная — failed),
 * редактор + «Запустить тесты» (POST /runs c task_id активной задачи),
 * вывод (тесты со статусами, stdout/stderr), ИИ-ревью, счётчик «Сдано X из N».
 * Код каждой задачи сохраняется в codeCache (key += task_id) — переживает
 * переключение вкладок и смену стадий.
 * Редактор инжинирится (prop `editor`), чтобы логику можно было
 * тестировать в jsdom без Monaco.
 */
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { ApiError, runTests, type RunResult, type Stack } from '../../api';
import type { StageTask } from '../../ws';
import { apiErrorMessage } from '../LoginView';
import { CodeEditor } from './CodeEditor';
import { parseGoTestJSON } from './goTestDigest';

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
 * Код кандидата + результат последнего запуска в рамках SPA (per задача).
 * Ключ кодирует сессию + стек + task_id + набор файлов задачи: повторный вход
 * в ту же задачу восстанавливает код, другая задача/стек — нет (своё ключевое
 * пространство, без чужого кода).
 */
interface CodeCacheEntry {
  files: Record<string, string>;
  result: RunResult | null;
}
const codeCache = new Map<string, CodeCacheEntry>();

function cacheKeyFor(
  sessionId: number,
  stack: Stack,
  taskId: string | undefined,
  taskFiles?: Record<string, string> | null,
): string {
  const keySet =
    taskFiles !== undefined && taskFiles !== null && Object.keys(taskFiles).length > 0
      ? Object.keys(taskFiles)
          .sort()
          .join('\u0000')
      : 'default';
  return `${sessionId}|${stack}|${taskId ?? 'none'}|${keySet}`;
}

export interface LiveCodePanelProps {
  sessionId: number;
  stack: Stack;
  /** Задачи стадии (stage.tasks, spec-13B T2): список задач сессии. */
  tasks?: StageTask[];
  /** Задача стадии (stage.task) — совместимость: одна задача, если tasks не задан. */
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
  tasks,
  taskId,
  taskTitle,
  taskFiles,
  editor,
  review,
}: LiveCodePanelProps) {
  // Список задач: stage.tasks; без него — одна задача из legacy-props (совместимость).
  const taskList = useMemo<StageTask[]>(() => {
    if (tasks !== undefined && tasks.length > 0) return tasks;
    if (taskId !== undefined) {
      return [{ id: taskId, title: taskTitle, files: taskFiles ?? undefined }];
    }
    return [];
  }, [tasks, taskId, taskTitle, taskFiles]);
  const nTasks = taskList.length;

  // Активная вкладка: старт — первая задача (spec-13B T2.4).
  const [activeIdx, setActiveIdx] = useState(0);
  const activeIdxSafe = nTasks > 0 ? Math.min(activeIdx, nTasks - 1) : 0;
  const activeTask = nTasks > 0 ? taskList[activeIdxSafe] : undefined;
  const activeTaskId = activeTask?.id;
  const hasTaskFiles =
    activeTask?.files !== undefined && activeTask?.files !== null &&
    Object.keys(activeTask.files).length > 0;

  const cacheKey = cacheKeyFor(sessionId, stack, activeTaskId, activeTask?.files);

  // Файлы активной задачи: из codeCache (свои правки) или файлы задачи/шаблон.
  const [files, setFiles] = useState<Record<string, string>>(() => {
    const cached = codeCache.get(cacheKeyFor(sessionId, stack, activeTaskId, activeTask?.files));
    if (cached !== undefined && Object.keys(cached.files).length > 0) return cached.files;
    if (hasTaskFiles) return activeTask!.files as Record<string, string>;
    return { [MAIN_FILE[stack]]: DEFAULT_CODE[stack] };
  });
  const [activeFile, setActiveFile] = useState<string>(() => solutionFile(stack, files));

  // Переключение вкладки (или приход stage.tasks после монтирования) —
  // восстанавливаем файлы именно этой задачи.
  useEffect(() => {
    const cached = codeCache.get(cacheKey);
    if (cached !== undefined && Object.keys(cached.files).length > 0) {
      setFiles(cached.files);
      setActiveFile(solutionFile(stack, cached.files));
      return;
    }
    if (hasTaskFiles) {
      setFiles(activeTask!.files as Record<string, string>);
      setActiveFile(solutionFile(stack, activeTask!.files));
      return;
    }
    const def = { [MAIN_FILE[stack]]: DEFAULT_CODE[stack] };
    setFiles(def);
    setActiveFile(solutionFile(stack, def));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cacheKey]);

  // Результаты per задача (для статус-точек и счётчика): из codeCache.
  const [results, setResults] = useState<Record<string, RunResult | null>>(() => {
    const m: Record<string, RunResult | null> = {};
    for (const t of taskList) {
      if (t.id === undefined) continue;
      m[t.id] =
        codeCache.get(cacheKeyFor(sessionId, stack, t.id, t.files))?.result ?? null;
    }
    return m;
  });
  const activeResult = activeTaskId !== undefined ? results[activeTaskId] ?? null : null;

  // spec-15 UX: разбор сырого go test -json в читабельный дайджест (диагностика
  // проваленных тестов терялась в стене машинного JSON). Не-json вывод
  // (pytest, ошибки компиляции) — как есть.
  const activeDigest = useMemo(
    () => (activeResult !== null ? parseGoTestJSON(activeResult.stdout ?? '') : null),
    [activeResult],
  );
  const digestOutput = useMemo(() => {
    const m = new Map<string, string>();
    if (activeDigest !== null) {
      for (const t of activeDigest.tests) {
        if (t.output.length > 0) m.set(t.name, t.output.join('\n'));
      }
    }
    return m;
  }, [activeDigest]);

  // spec-14/D7: ресинк по поздним/обновлённым stage.tasks — инициализация
  // once из taskList не видела задачи, пришедшие после монтирования (статус-
  // точки и счётчик «Сдано» не пересчитывались).
  useEffect(() => {
    setResults((prev) => {
      const next: Record<string, RunResult | null> = {};
      let changed = false;
      for (const t of taskList) {
        if (t.id === undefined) continue;
        const cached = codeCache.get(cacheKeyFor(sessionId, stack, t.id, t.files))?.result ?? null;
        next[t.id] = cached;
        if (prev[t.id] === undefined || prev[t.id] !== cached) changed = true;
      }
      return changed ? next : prev;
    });
  }, [taskList, sessionId, stack]);

  const [running, setRunning] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // spec-14/D7: setState после unmount (поздний ответ runTests) — guard.
  const mountedRef = useRef(true);
  useEffect(() => {
    mountedRef.current = true; // StrictMode: remount после cleanup
    return () => {
      mountedRef.current = false;
    };
  }, []);

  // Сохраняем код + результат активной задачи в кэш: переживает переключение
  // вкладок и смену стадий.
  useEffect(() => {
    codeCache.set(cacheKey, { files, result: activeResult });
  }, [cacheKey, files, activeResult]);

  const onRun = useCallback(async () => {
    if (running) return;
    setRunning(true);
    setError(null);
    try {
      const r = await runTests(sessionId, files, activeTaskId);
      if (activeTaskId !== undefined && mountedRef.current) {
        setResults((prev) => ({ ...prev, [activeTaskId as string]: r }));
      }
    } catch (e) {
      if (e instanceof ApiError && (e.code === 'sandbox_error' || e.code === 'sandbox_unavailable')) {
        setError(`Sandbox: ${e.message}`);
      } else {
        setError(apiErrorMessage(e));
      }
    } finally {
      if (mountedRef.current) setRunning(false);
    }
  }, [running, sessionId, files, activeTaskId]);

  const Editor = editor ?? defaultEditor;

  const testStats = useMemo(() => {
    if (activeResult === null) return null;
    const tests = activeResult.tests ?? []; // BUG-20261011-013: защита от null (backend)
    const passed = tests.filter((t) => t.passed).length;
    return { passed, total: tests.length };
  }, [activeResult]);

  // Сдано задач: задачи с ≥1 сдачей (spec-13B T2.3).
  const submitted = useMemo(
    () => taskList.filter((t) => t.id !== undefined && results[t.id] !== null && results[t.id] !== undefined).length,
    [taskList, results],
  );

  const taskDot = (t: StageTask): string => {
    if (t.id === undefined) return 'none';
    const r = results[t.id];
    if (r === null || r === undefined) return 'none';
    return r.passed ? 'ok' : 'fail';
  };

  return (
    <div className="livecode" data-testid="livecode">
      {nTasks > 1 && (
        <div className="task-tabs" role="tablist" aria-label="Задачи">
          {taskList.map((t, i) => (
            <button
              key={t.id ?? i}
              type="button"
              role="tab"
              aria-selected={i === activeIdxSafe}
              className={i === activeIdxSafe ? 'task-tab active' : 'task-tab'}
              onClick={() => {
                setActiveIdx(i);
                setError(null);
              }}
              data-testid={`task-tab-${i + 1}`}
            >
              <span className={`task-dot ${taskDot(t)}`} aria-hidden />
              Задача {i + 1}{t.title !== undefined && t.title !== '' ? ` · ${t.title}` : ''}
            </button>
          ))}
        </div>
      )}

      {activeTask !== undefined && (
        <p className="task-title" data-testid="task-title">
          {activeTask.title ?? activeTask.id}
        </p>
      )}

      {nTasks > 1 && (
        <p className="submitted-counter" data-testid="submitted-counter">
          Сдано задач: {submitted} из {nTasks}
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
          <span className={activeResult !== null && activeResult.passed ? 'run-ok' : 'run-fail'}>
            Тесты: {testStats.passed}/{testStats.total}
            {activeResult !== null && ` · ${activeResult.duration_ms} мс`}
          </span>
        )}
      </div>

      {error !== null && (
        <p className="form-error" role="alert" data-testid="run-error">
          {error}
        </p>
      )}

      {activeResult !== null && (
        <div className="run-output" data-testid="run-output">
          {(activeResult.tests ?? []).length > 0 && (
            <ul className="test-list">
              {(activeResult.tests ?? []).map((t) => (
                <li key={t.name} className={t.passed ? 'test-pass' : 'test-fail'}>
                  {t.passed ? '✓' : '✗'} {t.name}
                  {/* spec-15: диагностика проваленного теста — из per-test output
                      контракта api (если заполнен) либо из разобранного
                      go test -json (раньше она была только в сыром JSON-стиме). */}
                  {(() => {
                    const out = t.output !== undefined && t.output !== '' ? t.output : digestOutput.get(t.name);
                    return out !== undefined && out !== '' && <pre className="test-output">{out}</pre>;
                  })()}
                </li>
              ))}
            </ul>
          )}
          {activeDigest !== null && activeDigest.tests.length > 0 && (activeResult.tests ?? []).length === 0 && (
            <ul className="test-list">
              {activeDigest.tests.map((t) => (
                <li key={t.name} className={t.passed === true ? 'test-pass' : 'test-fail'}>
                  {t.passed === true ? '✓' : '✗'} {t.name}
                  {t.output.length > 0 && <pre className="test-output">{t.output.join('\n')}</pre>}
                </li>
              ))}
            </ul>
          )}
          {activeDigest !== null ? (
            <>
              {activeDigest.packages.length > 0 && (
                <pre className="run-stdout">{activeDigest.packages.join('\n')}</pre>
              )}
              <details className="run-raw">
                <summary>сырой вывод тестов</summary>
                <pre>{activeDigest.raw}</pre>
              </details>
            </>
          ) : (
            (activeResult.stdout !== '' || activeResult.stderr !== '') && (
              <pre className="run-stdout">{[activeResult.stdout, activeResult.stderr].filter(Boolean).join('\n')}</pre>
            )
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
