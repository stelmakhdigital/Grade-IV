import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { LiveCodePanel, DEFAULT_CODE } from './LiveCodePanel';

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status });
}

const RUN_OK = {
  exit_code: 0,
  stdout: 'PASS',
  stderr: '',
  duration_ms: 25,
  passed: true,
  tests: [
    { name: 'TestMain', passed: true },
    { name: 'TestEdge', passed: false, output: 'ожидалось 1, получено 0' },
  ],
};

/** Простейший редактор-заглушка вместо Monaco (jsdom). */
function stubEditor() {
  return function StubEditor(props: {
    language: string;
    value: string;
    onChange: (v: string) => void;
  }) {
    return (
      <textarea
        data-testid="editor"
        data-lang={props.language}
        value={props.value}
        onChange={(e) => props.onChange(e.target.value)}
        rows={4}
      />
    );
  };
}

const TASK_FILES = {
  'go.mod': 'module task\n\ngo 1.21\n',
  'solution.go': 'package task\n\nfunc Reverse(s string) string { return "" }\n',
  'solution_test.go': 'package task\n\nfunc TestReverse(t *testing.T) {}\n',
};

function renderPanel(opts: {
  fetchMock?: unknown;
  review?: string;
  taskTitle?: string;
  taskFiles?: Record<string, string> | null;
} = {}) {
  vi.stubGlobal(
    'fetch',
    opts.fetchMock ??
      vi.fn(async (url: string, init?: RequestInit) => {
        if (init?.method === 'POST' && url.endsWith('/runs')) {
          return json(200, RUN_OK);
        }
        return json(404, {});
      }),
  );
  return render(
    <LiveCodePanel
      sessionId={9}
      stack="go"
      taskId="go-rev"
      taskTitle={opts.taskTitle}
      taskFiles={opts.taskFiles}
      review={opts.review ?? ''}
      editor={stubEditor()}
    />,
  );
}

beforeEach(() => {
  vi.unstubAllGlobals();
});

describe('LiveCodePanel (WP-9)', () => {
  it('дефолтный код Go, заголовок задачи, кнопка запуска', () => {
    renderPanel({ taskTitle: 'Разворот строки' });
    expect(screen.getByTestId('editor')).toHaveValue(DEFAULT_CODE.go);
    expect(screen.getByTestId('task-title')).toHaveTextContent('Разворот строки');
    expect(screen.getByTestId('run-tests')).toHaveTextContent('Запустить тесты');
  });

  it('запуск: POST с файлом main.go и task_id; вывод тестов', async () => {
    const fetchMock = vi.fn().mockResolvedValue(json(200, RUN_OK));
    vi.stubGlobal('fetch', fetchMock);
    renderPanel({ fetchMock: fetchMock });
    const user = userEvent.setup();
    await user.click(screen.getByTestId('run-tests'));
    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/v1/sessions/9/runs',
        expect.objectContaining({ method: 'POST' }),
      ),
    );
    const body = JSON.parse((fetchMock.mock.calls[0][1] as RequestInit).body as string);
    expect(body).toEqual({
      files: { 'main.go': DEFAULT_CODE.go },
      action: 'test',
      task_id: 'go-rev',
    });
    expect(await screen.findByText(/Тесты: 1\/2/)).toBeInTheDocument();
    expect(screen.getByText('✓ TestMain')).toBeInTheDocument();
    expect(screen.getByText(/✗ TestEdge/)).toBeInTheDocument();
    expect(screen.getByText('ожидалось 1, получено 0')).toBeInTheDocument();
  });

  it('ошибка sandbox — человекочитаемое сообщение', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      json(502, { code: 'sandbox_error', msg: 'контейнер не создан' }),
    );
    vi.stubGlobal('fetch', fetchMock);
    renderPanel({ fetchMock: fetchMock });
    const user = userEvent.setup();
    await user.click(screen.getByTestId('run-tests'));
    expect(await screen.findByTestId('run-error')).toHaveTextContent(
      'Sandbox: контейнер не создан',
    );
  });

  it('ИИ-ревью отображается, если передано', () => {
    renderPanel({ review: 'Хорошее решение, но добавьте обработку пустого входа.' });
    expect(screen.getByTestId('ai-review')).toHaveTextContent(
      'добавьте обработку пустого входа',
    );
  });

  it('файлы задачи: вкладки, активный solution.go, /runs с полным набором', async () => {
    const fetchMock = vi.fn().mockResolvedValue(json(200, RUN_OK));
    vi.stubGlobal('fetch', fetchMock);
    renderPanel({ fetchMock, taskFiles: TASK_FILES, taskTitle: 'Разворот строки' });
    // вкладки всех файлов задачи
    expect(screen.getByRole('tab', { name: 'go.mod' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'solution_test.go' })).toBeInTheDocument();
    // активен файл решения, его содержимое в редакторе
    expect(screen.getByRole('tab', { name: 'solution.go' })).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByTestId('editor')).toHaveValue(TASK_FILES['solution.go']);
    const user = userEvent.setup();
    await user.click(screen.getByTestId('run-tests'));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    const body = JSON.parse((fetchMock.mock.calls[0][1] as RequestInit).body as string);
    expect(body.files).toEqual(TASK_FILES);
    expect(body.task_id).toBe('go-rev');
  });

  it('python-стек: main.py и python-синтаксис', () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => json(200, { ...RUN_OK, tests: [] })),
    );
    render(
      <LiveCodePanel sessionId={9} stack="python" editor={stubEditor()} review="" />,
    );
    expect(screen.getByTestId('editor')).toHaveValue(DEFAULT_CODE.python);
    expect(screen.getByTestId('editor')).toHaveAttribute('data-lang', 'python');
  });
});
