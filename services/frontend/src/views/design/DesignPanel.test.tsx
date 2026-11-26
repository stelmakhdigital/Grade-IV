import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useEffect } from 'react';
import type { ComponentType } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { DESIGN_BLOCKS, DesignPanel, type CanvasProps } from './DesignPanel';

interface MockApi {
  placeBlock: ReturnType<typeof vi.fn>;
  snapshot: () => Promise<{
    state: { elements: unknown[]; appState: unknown };
    arrows: number;
    pngB64?: string | null;
  }>;
}

/** Холст-заглушка вместо Excalidraw (jsdom). */
function makeMockCanvas() {
  const calls: string[] = [];
  const api: MockApi = {
    placeBlock: vi.fn((name: string) => calls.push(name)),
    snapshot: () =>
      Promise.resolve({
        state: {
          elements: calls.map((n, i) => ({ id: `e${i}`, type: 'rectangle', text: n })),
          appState: { viewBackgroundColor: '#111' },
        },
        arrows: 2,
        pngB64: 'aW1hZ2U=',
      }),
  };
  const Canvas: ComponentType<CanvasProps> = ({ apiRef, initialState }) => {
    useEffect(() => {
      apiRef.current = api;
      return () => {
        apiRef.current = null;
      };
    }, [apiRef, api]);
    const restored =
      initialState !== undefined && initialState !== null && Array.isArray(initialState.elements)
        ? `|saved:${initialState.elements.length}`
        : '';
    return <div data-testid="mock-canvas">{`${calls.join(',')}${restored}`}</div>;
  };
  return { api, Canvas, calls };
}

/** Маршруты холста: по умолчанию — сохранённой схемы нет (GET 404), PUT принят. */
const WHITEBOARD_OK = { 'PUT /whiteboard': { saved: true, structure: { blocks: [], links: 0 } } };

function renderPanel(opts: {
  routes?: Record<string, unknown>;
  review?: string;
  canvas?: { api: MockApi; Canvas: ComponentType<CanvasProps> };
} = {}) {
  const mock = opts.canvas ?? makeMockCanvas();
  const fetchMock = __api.on(opts.routes ?? WHITEBOARD_OK);
  return {
    ...mock,
    fetchMock,
    utils: render(
      <DesignPanel
        sessionId={7}
        canvas={mock.Canvas}
        review={opts.review ?? ''}
      />,
    ),
  };
}

beforeEach(() => {
  vi.unstubAllGlobals();
});

/** Вызовы PUT: при входе на стадию панель ещё делает GET сохранённой схемы. */
function putCalls(mock: ReturnType<typeof vi.fn>): unknown[][] {
  return mock.mock.calls.filter(([, init]) => (init as RequestInit | undefined)?.method === 'PUT');
}

describe('DesignPanel (WP-10)', () => {
  it('палитра: 12 блоков ADR-004', () => {
    renderPanel();
    expect(DESIGN_BLOCKS).toHaveLength(12);
    for (const name of DESIGN_BLOCKS) {
      expect(screen.getByRole('button', { name })).toBeInTheDocument();
    }
  });

  it('клик по блоку — вставка на холст', async () => {
    const { api, utils } = renderPanel();
    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'SQL-БД' }));
    expect(api.placeBlock).toHaveBeenCalledWith('SQL-БД');
    expect(utils.container.querySelector('[data-testid="mock-canvas"]')).toHaveTextContent(
      'SQL-БД',
    );
  });

  it('«Оценить» — PUT /whiteboard со state и структурой', async () => {
    const { api, fetchMock } = renderPanel({ routes: { '/whiteboard': { saved: true } } });
    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'Client' }));
    await user.click(screen.getByRole('button', { name: 'Кэш' }));
    await user.click(screen.getByTestId('design-evaluate'));
    await waitFor(() => expect(putCalls(fetchMock)).toHaveLength(1));
    const [url, init] = putCalls(fetchMock)[0];
    expect(String(url)).toContain('/api/v1/sessions/7/whiteboard');
    expect((init as RequestInit).method).toBe('PUT');
    const body = JSON.parse((init as RequestInit).body as string);
    expect(body.structure).toEqual({ blocks: ['Client', 'Кэш'], links: 2 });
    expect(Array.isArray(body.state.elements)).toBe(true);
    expect(body.state.elements).toHaveLength(2);
    await waitFor(() =>
      expect(screen.getByTestId('design-evaluate')).toHaveTextContent(
        'Схема сохранена — ждём оценку',
      ),
    );
    void api;
  });

  it('вход на стадию — сохранённая схема возвращается на холст (переживает перезагрузку)', async () => {
    const saved = {
      state: { elements: [{ id: 'x', type: 'rectangle' }], appState: {} },
      structure: { blocks: ['Кэш'], links: 1 },
    };
    const mock = makeMockCanvas();
    renderPanel({ routes: { '/whiteboard': saved }, canvas: mock });
    await waitFor(() => expect(screen.getByTestId('mock-canvas')).toHaveTextContent('saved:1'));
    // счётчик блоков тоже восстанавливается, иначе «Оценить» остаётся заблокированной
    expect(screen.getByText('Блоков на холсте: 1')).toBeInTheDocument();
  });

  it('кнопка «Оценить» заблокирована без блоков', () => {
    renderPanel();
    expect(screen.getByTestId('design-evaluate')).toBeDisabled();
  });

  it('ошибка сохранения — сообщение об ошибке', async () => {
    const { api } = renderPanel({
      routes: { '/whiteboard': __api.failure(500, 'internal', 'ошибка сохранения холста') },
    });
    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'CDN' }));
    await user.click(screen.getByTestId('design-evaluate'));
    await waitFor(() =>
      expect(screen.getByTestId('design-error')).toHaveTextContent(
        'Схема: ошибка сохранения холста',
      ),
    );
    void api;
  });

  it('ревью ИИ — блок «Оценка ИИ»', () => {
    renderPanel({ review: 'Хорошая схема: есть кэш и очередь. Follow-up: …' });
    expect(screen.getByTestId('design-review')).toHaveTextContent(
      'Хорошая схема: есть кэш и очередь',
    );
  });
});
