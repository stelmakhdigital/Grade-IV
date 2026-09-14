/**
 * Холст System Design на Excalidraw (ADR-004, WP-10): свободное рисование +
 * вставка блоков палитры (группа «прямоугольник + подпись»). Динамическая
 * загрузка — тяжёлый бандл не попадает в критический путь кабинета.
 */
import { Suspense, lazy, useEffect, useId, useRef } from 'react';
import type { DesignCanvasApi } from './DesignCanvasApi';

const Excalidraw = lazy(() =>
  import('@excalidraw/excalidraw').then((m) => ({ default: m.Excalidraw })),
);

export interface ExcalidrawCanvasProps {
  apiRef: { current: DesignCanvasApi | null };
}

// Цвета блоков палитры (ADR-004).
const PALETTE_COLORS = [
  'hsl(210, 90%, 70%)', 'hsl(45, 95%, 60%)', 'hsl(150, 70%, 55%)', 'hsl(0, 80%, 65%)',
  'hsl(275, 75%, 65%)', 'hsl(330, 75%, 60%)',
];

let idCounter = 0;
function nextId(prefix: string): string {
  idCounter += 1;
  return `${prefix}-${Date.now().toString(36)}-${idCounter}`;
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type SceneElement = any;

/** Группа элементов для блока: прямоугольник + текст-подпись. */
function blockElements(name: string, x: number, y: number): SceneElement[] {
  const base = {
    angle: 0,
    strokeColor: '#e6e6e6',
    fillStyle: 'solid',
    strokeWidth: 1,
    strokeStyle: 'solid',
    groupIds: [],
    frameId: null,
    roundness: { type: 3 },
    opacity: 100,
    seed: 1,
    version: 1,
    versionNonce: Math.floor(Math.random() * 1e9),
    isDeleted: false,
    boundElements: null,
    updated: Date.now(),
    index: null,
  };
  const rect: SceneElement = {
    ...base,
    id: nextId('rect'),
    type: 'rectangle',
    x,
    y,
    width: 180,
    height: 60,
    backgroundColor: PALETTE_COLORS[Math.floor(Math.random() * PALETTE_COLORS.length)] ?? '#888',
  };
  const text: SceneElement = {
    ...base,
    id: nextId('txt'),
    type: 'text',
    x: x + 12,
    y: y + 18,
    width: 156,
    height: 24,
    text: name,
    fontSize: 20,
    fontFamily: 1,
    textAlign: 'left',
    baseline: 18,
    containerId: null,
    originalText: name,
    autoResize: true,
  };
  return [rect, text];
}

export function ExcalidrawCanvas({ apiRef }: ExcalidrawCanvasProps) {
  const label = useId();
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const excalidraw = useRef<any>(null);
  const slot = useRef(0);

  useEffect(() => {
    apiRef.current = {
      placeBlock: (name: string) => {
        const api = excalidraw.current;
        if (api === null) return;
        const row = Math.floor(slot.current / 4);
        const col = slot.current % 4;
        slot.current += 1;
        const x = 60 + col * 220;
        const y = 80 + row * 120;
        // Excalidraw 0.18: addElements убран — расширяем сцену через updateScene.
        const elements: SceneElement[] = api.getSceneElements() ?? [];
        api.updateScene({ elements: [...elements, ...blockElements(name, x, y)] });
      },
      snapshot: () => {
        const api = excalidraw.current;
        const elements: SceneElement[] = api?.getSceneElements() ?? [];
        return {
          state: { elements, appState: api?.getAppState() ?? {} },
          arrows: elements.filter((e) => e?.type === 'arrow').length,
        };
      },
    };
    return () => {
      apiRef.current = null;
    };
  }, [apiRef]);

  return (
    <div className="design-canvas">
      <Suspense fallback={<div className="muted">Загрузка холста…</div>}>
        <Excalidraw
          key={label}
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          excalidrawAPI={(api: any) => {
            excalidraw.current = api;
          }}
          theme="dark"
          langCode="ru"
        />
      </Suspense>
    </div>
  );
}
