import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  LiveCodePanel,
  DEFAULT_CODE,
  DRAFT_DEBOUNCE_MS,
  codeCache,
  saveDraft,
  restoreDraft,
} from './LiveCodePanel';

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
  review?: string;
  taskTitle?: string;
  taskFiles?: Record<string, string> | null;
} = {}) {
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
  // панель чернит код в sessionStorage — тесты не должны видеть черновики друг друга
  sessionStorage.clear();
  // номер активной задачи живёт в адресе: без чистого адреса тесты читают
  // ?t= после соседа и стартуют не с первой вкладки
  window.location.hash = '#/sessions/77';
  // codeCache — модульный синглтон: без очистки следующий тест получает
  // отредактированный код соседа (вперемешку падал «старт — вкладка 1»)
  codeCache.clear();
});

describe('LiveCodePanel (WP-9)', () => {
  it('дефолтный код Go, заголовок задачи, кнопка запуска', () => {
    renderPanel({ taskTitle: 'Разворот строки' });
    expect(screen.getByTestId('editor')).toHaveValue(DEFAULT_CODE.go);
    expect(screen.getByTestId('task-title')).toHaveTextContent('Разворот строки');
    expect(screen.getByTestId('run-tests')).toHaveTextContent('Запустить тесты');
  });

  it('запуск: POST с файлом main.go и task_id; вывод тестов', async () => {
    const fetchMock = __api.on({ 'POST /runs': RUN_OK });
    renderPanel();
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
    __api.on({ 'POST /runs': __api.failure(502, 'sandbox_error', 'контейнер не создан') });
    renderPanel();
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
    const fetchMock = __api.on({ 'POST /runs': RUN_OK });
    renderPanel({ taskFiles: TASK_FILES, taskTitle: 'Разворот строки' });
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
    render(
      <LiveCodePanel sessionId={9} stack="python" editor={stubEditor()} review="" />,
    );
    expect(screen.getByTestId('editor')).toHaveValue(DEFAULT_CODE.python);
    expect(screen.getByTestId('editor')).toHaveAttribute('data-lang', 'python');
  });

  it('черновик кода переживает перезагрузку (sessionStorage, не только память вкладки)', async () => {
    const user = userEvent.setup();
    // новый render: в памяти вкладки кода этой сессии нет — спасает только хранилище
    const { unmount } = render(
      <LiveCodePanel
        sessionId={91}
        stack="go"
        tasks={[{ id: 'go-t9', title: 'Девятая', files: { 'solution.go': 'package task // шаблон' } }]}
        review=""
        editor={stubEditor()}
      />,
    );
    await user.clear(screen.getByTestId('editor'));
    await user.type(screen.getByTestId('editor'), 'package draft // из хранилища');
    // запись в хранилище дебаунсится; закрытие панели (страницу закрывают/
    // перезагружают) сливает незаписанный черновик — это и есть путь F5
    unmount();
    expect(JSON.parse(sessionStorage.getItem('grade.code.91') ?? '{}')['go-t9'])
      .toEqual({ 'solution.go': 'package draft // из хранилища' });
    // и читается по своему ключу задачи
    expect(restoreDraft(91, 'go-t9')).toEqual({ 'solution.go': 'package draft // из хранилища' });
    expect(restoreDraft(91, 'go-другая')).toBeUndefined();
  });

  describe('запись черновиков в хранилище', () => {
    afterEach(() => {
      vi.useRealTimers();
    });

    it('не сериализует состояние на каждое нажатие клавиши', () => {
      vi.useFakeTimers();
      const setItem = vi.spyOn(Storage.prototype, 'setItem');
      try {
        render(
          <LiveCodePanel sessionId={92} stack="go" taskId="go-rev" review="" editor={stubEditor()} />,
        );
        const editor = screen.getByTestId('editor');
        // 5 «нажатий»: каждый onChange — один символ, добавленный к коду
        let value = DEFAULT_CODE.go;
        for (const ch of '12345') {
          value += ch;
          fireEvent.change(editor, { target: { value } });
        }
        // правки сразу в редакторе (это источник правды), но не в хранилище
        expect(editor).toHaveValue(value);
        expect(setItem).not.toHaveBeenCalled();
        // прошла пауза debounce — одна пачечная запись на все 5 нажатий
        vi.advanceTimersByTime(DRAFT_DEBOUNCE_MS);
        expect(setItem).toHaveBeenCalledTimes(1);
        expect(restoreDraft(92, 'go-rev')).toEqual({ 'main.go': value });
        // без правок хранилище не перетирается
        vi.advanceTimersByTime(DRAFT_DEBOUNCE_MS * 3);
        expect(setItem).toHaveBeenCalledTimes(1);
      } finally {
        setItem.mockRestore();
      }
    });

    it('черновик одной задачи не затирает черновик другой', async () => {
      const user = userEvent.setup();
      const { unmount } = render(
        <LiveCodePanel
          sessionId={93}
          stack="go"
          tasks={[
            { id: 'go-t1', title: 'Первая', files: { 'solution.go': 'package task // t1' } },
            { id: 'go-t2', title: 'Вторая', files: { 'solution.go': 'package task // t2' } },
          ]}
          review=""
          editor={stubEditor()}
        />,
      );
      await user.clear(screen.getByTestId('editor'));
      await user.type(screen.getByTestId('editor'), 'package t1 // черновик');
      // переключение задачи — черновик первой сливается сразу, без таймаута
      await user.click(screen.getByTestId('task-tab-2'));
      await user.clear(screen.getByTestId('editor'));
      await user.type(screen.getByTestId('editor'), 'package t2 // черновик');
      unmount(); // закрытие страницы — сливается черновик второй

      const drafts = JSON.parse(sessionStorage.getItem('grade.code.93') ?? '{}');
      expect(drafts['go-t1']).toEqual({ 'solution.go': 'package t1 // черновик' });
      expect(drafts['go-t2']).toEqual({ 'solution.go': 'package t2 // черновик' });
    });

    it('черновики скрытых задач чистятся, активной — остаётся', () => {
      // черновик задачи прошлой стадии (её больше не показывают) и текущей
      saveDraft(77, 'go-old', { 'main.go': 'package old // прошлая стадия' });
      saveDraft(77, 'go-t1', { 'solution.go': 'package t1 // из хранилища' });
      render(
        <LiveCodePanel
          sessionId={77}
          stack="go"
          tasks={[
            { id: 'go-t1', title: 'Первая', files: { 'solution.go': 'package task // t1' } },
            { id: 'go-t2', title: 'Вторая', files: { 'solution.go': 'package task // t2' } },
          ]}
          review=""
          editor={stubEditor()}
        />,
      );
      expect(restoreDraft(77, 'go-old')).toBeUndefined(); // скрытая — вычищена
      // активная задача не съедена очисткой и её черновик восстановлен в редактор
      expect(restoreDraft(77, 'go-t1')).toEqual({ 'solution.go': 'package t1 // из хранилища' });
      expect(screen.getByTestId('editor')).toHaveValue('package t1 // из хранилища');
    });
  });

  describe('вкладки задач (spec-13B T2)', () => {
    const T1 = {
      id: 'go-t1',
      title: 'Первая',
      files: { 'solution.go': 'package task // t1' },
    };
    const T2 = {
      id: 'go-t2',
      title: 'Вторая',
      files: { 'solution.go': 'package task // t2' },
    };

    const renderTwoTasks = () => {
      return render(
        <LiveCodePanel
          sessionId={77}
          stack="go"
          tasks={[T1, T2]}
          review=""
          editor={stubEditor()}
        />,
      );
    };

    it('N вкладки, старт — вкладка 1, счётчик «Сдано 0 из 2»', () => {
      renderTwoTasks();
      expect(screen.getByTestId('task-tab-1')).toHaveAttribute('aria-selected', 'true');
      expect(screen.getByTestId('task-tab-2')).toHaveAttribute('aria-selected', 'false');
      expect(screen.getByTestId('task-tab-1')).toHaveTextContent('Задача 1');
      expect(screen.getByTestId('task-tab-2')).toHaveTextContent('Вторая');
      // старт — файлы задачи 1
      expect(screen.getByTestId('editor')).toHaveValue('package task // t1');
      expect(screen.getByTestId('submitted-counter')).toHaveTextContent('Сдано задач: 0 из 2');
    });


    it('клик по вкладке пишет номер задачи в адрес', async () => {
      renderTwoTasks();
      const user = userEvent.setup();
      await user.click(screen.getByTestId('task-tab-2'));
      expect(window.location.hash).toBe('#/sessions/77?t=2');
      expect(screen.getByTestId('editor')).toHaveValue('package task // t2');
    });

    it('открытая по ссылке задача восстанавливается (?t=2)', () => {
      window.location.hash = '#/sessions/77?t=2';
      renderTwoTasks();
      expect(screen.getByTestId('task-tab-2')).toHaveAttribute('aria-selected', 'true');
      expect(screen.getByTestId('editor')).toHaveValue('package task // t2');
    });

    it('код сохраняется при переключении вкладок (task_id в ключе кэша)', async () => {
      const user = userEvent.setup();
      renderTwoTasks();
      // правим код задачи 1
      const editor = screen.getByTestId('editor');
      await user.clear(editor);
      await user.type(editor, 'package task // t1 моё решение');
      expect(editor).toHaveValue('package task // t1 моё решение');
      // переход на задачу 2 — у неё свои файлы
      await user.click(screen.getByTestId('task-tab-2'));
      expect(editor).toHaveValue('package task // t2');
      // возврат на задачу 1 — код на месте
      await user.click(screen.getByTestId('task-tab-1'));
      expect(editor).toHaveValue('package task // t1 моё решение');
    });

    it('«Запустить тесты» — /runs с task_id активной задачи; статус-точка и счётчик', async () => {
      const fetchMock = __api.on({ 'POST /runs': RUN_OK });
      const user = userEvent.setup();
      render(
        <LiveCodePanel
          sessionId={78}
          stack="go"
          tasks={[T1, T2]}
          review=""
          editor={stubEditor()}
        />,
      );
      // сначала задача 2 — запуск с её task_id
      await user.click(screen.getByTestId('task-tab-2'));
      await user.click(screen.getByTestId('run-tests'));
      await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
      const body = JSON.parse((fetchMock.mock.calls[0][1] as RequestInit).body as string);
      expect(body.task_id).toBe('go-t2');
      expect(body.files).toEqual(T2.files);
      // зелёная точка у задачи 2, серая у задачи 1; счётчик 1 из 2
      await waitFor(() =>
        expect(screen.getByTestId('task-tab-2')).toHaveTextContent('Вторая'),
      );
      expect(screen.getByTestId('submitted-counter')).toHaveTextContent('Сдано задач: 1 из 2');
      // результат задачи 1 не затёрт: переход на 1 — без результата
      await user.click(screen.getByTestId('task-tab-1'));
      expect(screen.queryByTestId('run-output')).not.toBeInTheDocument();
      // возврат на 2 — результат на месте
      await user.click(screen.getByTestId('task-tab-2'));
      expect(screen.getByTestId('run-output')).toBeInTheDocument();
    });
  });
});
