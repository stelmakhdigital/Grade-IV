/**
 * Холст System Design на Excalidraw (ADR-004, WP-10): свободное рисование +
 * вставка блоков палитры (группа «прямоугольник + подпись»). Динамическая
 * загрузка — тяжёлый бандл не попадает в критический путь кабинета.
 */
import { Suspense, lazy, useEffect, useId, useRef } from 'react';
// СТИЛИ Excalidraw (BUG-20261011-017): без этого CSS тулбар кривой —
// keybinding-подписи (1-8) льются отдельной колонкой (нужен position:absolute
// + display:none), radio-инпуты внутри ToolIcon видны серыми точками.
// Импортируем здесь (не в main.tsx), чтобы CSS пошёл в design-чанк вместе
// с самим Excalidraw (code-splitting).
import '@excalidraw/excalidraw/index.css';
import type { WhiteboardState } from '../../api';
import type { DesignCanvasApi } from './DesignCanvasApi';
import { blockColor } from './designColors';

const Excalidraw = lazy(() =>
  import('@excalidraw/excalidraw').then((m) => ({ default: m.Excalidraw })),
);

// PNG-экспорт холста (ADR-004: оценка vision по PNG ≥ 1920 px по широкой стороне).
// Лениво: тянет exportToBlob из того же модуля.
async function exportCanvasPng(
  elements: unknown[],
  appState: unknown,
): Promise<string | null> {
  if (!elements.length) return null;
  try {
    const { exportToBlob } = await import('@excalidraw/excalidraw');
    const blob: Blob | undefined = await exportToBlob(
      {
        elements: elements as never,
        appState: appState as never,
        files: {} as never,
        exportPadding: 32,
      },
      { mimeType: 'image/png', quality: 0.9 },
    );
    if (!blob) return null;
    const dataUrl: string = await new Promise((resolve, reject) => {
      const fr = new FileReader();
      fr.onload = () => resolve(String(fr.result));
      fr.onerror = () => reject(fr.error);
      fr.readAsDataURL(blob);
    });
    return dataUrl.split(',', 2)[1] ?? null;
  } catch (err) {
    console.warn('Дизайн-холст: PNG-экспорт не удался', err);
    return null;
  }
}

export interface ExcalidrawCanvasProps {
  apiRef: { current: DesignCanvasApi | null };
  /** Ранее сохранённая схема (GET whiteboard) — восстанавливается в холст. */
  initialState?: WhiteboardState | null;
}

// Цвет блоков: детерминированно по имени (designColors) — палитра и холст
// одного цвета (раньше Math.random: один и тот же блок каждого цвета).

let idCounter = 0;
function nextId(prefix: string): string {
  idCounter += 1;
  return `${prefix}-${Date.now().toString(36)}-${idCounter}`;
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type SceneElement = any;

/** Группа элементов для блока: прямоугольник + текст-подпись.
 * Оба элемента в одной группе (groupIds) — подпись тянется за прямоугольником
 * (раньше текст «отставал» при перетаскивании блока). */
function blockElements(name: string, x: number, y: number): SceneElement[] {
  const gid = nextId('grp');
  const textId = nextId('txt');
  const rectId = nextId('rect');
  const base = {
    angle: 0,
    strokeColor: '#e6e6e6',
    fillStyle: 'solid',
    strokeWidth: 1,
    strokeStyle: 'solid',
    groupIds: [gid],
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
  // Подпись блока — связанный текст (containerId + boundElements): каноничный
  // «labeled rectangle» Excalidraw. Раньше текст был standalone (containerId:
  // null) — живой холст его не рендерил (блоки пустые), хотя exportCanvasPng
  // подписи рисовал (PNG для ИИ-ревью был с текстом, живой холст — без).
  const rect: SceneElement = {
    ...base,
    id: rectId,
    type: 'rectangle',
    x,
    y,
    width: 180,
    height: 60,
    backgroundColor: blockColor(name),
    boundElements: [{ id: textId, type: 'text' }],
  };
  const text: SceneElement = {
    ...base,
    id: textId,
    type: 'text',
    x: x + 12,
    y: y + 18,
    width: 156,
    height: 24,
    text: name,
    fontSize: 20,
    fontFamily: 1,
    textAlign: 'center',
    verticalAlign: 'middle',
    baseline: 18,
    containerId: rectId,
    originalText: name,
    autoResize: false,
  };
  return [rect, text];
}

export function ExcalidrawCanvas({ apiRef, initialState }: ExcalidrawCanvasProps) {
  const label = useId();
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const excalidraw = useRef<any>(null);
  const slot = useRef(0);
  // Очередь placeBlock: быстрые клики сериализуются (restore должен видеть
  // сцену с предыдущими блоками); clear() встаёт в конец очереди.
  const placeChain = useRef<Promise<void>>(Promise.resolve());
  // Схема, которую нужно загрузить, пока ленивый Excalidraw ещё не готов.
  const pendingRestore = useRef<WhiteboardState | null>(null);

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
        // Excalidraw 0.18: addElements убран. Сцену расширяем через
        // restoreElements (экспорт модуля, динамический импорт — как
        // exportToBlob, чтобы jsdom-тесты не тянули весь Excalidraw) +
        // updateScene: restoreElements нормализует элементы — корректные
        // fractional-индексы, шрифты текста и связи «подпись ↔ прямоугольник».
        // Голый updateScene с самодельными элементами текст НЕ рендерил
        // (блоки пустые), хотя exportCanvasPng подписи рисовал.
        const raw = [...(api.getSceneElements() ?? []), ...blockElements(name, x, y)];
        // Сериализация быстрых кликов: каждый restore должен видеть сцену
        // с предыдущими блоками (иначе последний overwrite теряет блоки).
        placeChain.current = placeChain.current.then(async () => {
          const { restoreElements } = await import('@excalidraw/excalidraw');
          api.updateScene({ elements: restoreElements(raw, null) });
        });
      },
      clear: () => {
        const api = excalidraw.current;
        if (api === null) return;
        slot.current = 0; // новые блоки — снова с первой ячейки сетки
        // Очистка после всех поставленных блоков (конец очереди placeBlock):
        // иначе блок, поставленный параллельно с очисткой, воскрес бы после неё.
        placeChain.current = placeChain.current.then(() => {
          api.updateScene({ elements: [] });
        });
      },
      snapshot: async () => {
        const api = excalidraw.current;
        const elements: SceneElement[] = api?.getSceneElements() ?? [];
        const appState = api?.getAppState() ?? {};
        const pngB64 = await exportCanvasPng(elements, appState);
        return {
          state: { elements, appState },
          arrows: elements.filter((e) => e?.type === 'arrow').length,
          pngB64,
        };
      },
    };
    return () => {
      apiRef.current = null;
    };
  }, [apiRef]);

  // Восстановление сохранённой схемы (GET whiteboard → холст): применяем, когда
  // Excalidraw готов; иначе держим в pendingRestore (применит excalidrawAPI-колбэк).
  useEffect(() => {
    if (initialState === null || initialState === undefined) return;
    const api = excalidraw.current;
    if (api !== null) {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      (api as any).updateScene({
        elements: initialState.elements ?? [],
        appState: initialState.appState ?? {},
      });
      pendingRestore.current = null;
    } else {
      pendingRestore.current = initialState;
    }
  }, [initialState]);

  return (
    <div className="design-canvas">
      <Suspense fallback={<div className="muted">Загрузка холста…</div>}>
        <Excalidraw
          key={label}
          // BUG-20261011-016: ширина контейнера .design-canvas ≈ 768 px — совпадает
          // с dockedSidebarBreakpoint (768) по умолчанию, Excalidraw переключался
          // в мобильный режим: тулбар с инструментами (стрелка/фигуры) прятался
          // за бургер-меню, соединить блоки стрелками было нельзя. 0 — всегда
          // desktop-раскладка.
          UIOptions={{ dockedSidebarBreakpoint: 0 }}
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          excalidrawAPI={(api: any) => {
            excalidraw.current = api;
            // Холст готов — применяем накопленное восстановление (если было).
            if (pendingRestore.current !== null) {
              api.updateScene({
                elements: pendingRestore.current.elements ?? [],
                appState: pendingRestore.current.appState ?? {},
              });
              pendingRestore.current = null;
            }
          }}
          theme="dark"
          langCode="ru"
        />
      </Suspense>
    </div>
  );
}
