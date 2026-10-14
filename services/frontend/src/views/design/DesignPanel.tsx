/**
 * Панель стадии System Design (ADR-004, WP-10): палитра 12 архитектурных
 * блоков (3 группы), холст (Excalidraw или инъектированный мок в тестах),
 * «Оценить» → PUT /whiteboard (state + структура) → ИИ-оценка (ai_text).
 */
import { useCallback, useEffect, useRef, useState } from 'react';
import type { ComponentType } from 'react';
import { ApiError, getWhiteboard, saveWhiteboard, type WhiteboardState } from '../../api';
import { apiErrorMessage } from '../LoginView';
import type { DesignCanvasApi } from './DesignCanvasApi';
import { blockColor } from './designColors';
import { ExcalidrawCanvas } from './ExcalidrawCanvas';

/** Палитра 12 блоков ADR-004, сгруппированно по роли в архитектуре.
 * Порядок групп: трафик → сервисы → данные/инфраструктура (логика чтения
 * схемы слева направо, сверху вниз). */
export const DESIGN_BLOCK_GROUPS = [
  { title: 'Трафик', names: ['Client', 'Load Balancer', 'CDN'] },
  { title: 'Сервисы', names: ['API-сервис', 'Worker', 'Внешний API'] },
  {
    title: 'Данные и инфраструктура',
    names: ['SQL-БД', 'NoSQL', 'Кэш', 'Очередь', 'Message Broker', 'Monitoring'],
  },
] as const;

/** Плоский список 12 блоков (ADR-004) — совместимость/использование вне палитры. */
export const DESIGN_BLOCKS: readonly string[] = DESIGN_BLOCK_GROUPS.flatMap(
  (g) => g.names,
);

export interface DesignPanelProps {
  sessionId: number;
  /** Холст: по умолчанию Excalidraw; в тестах — мок. */
  canvas?: ComponentType<CanvasProps>;
  /** ИИ-оценка (последний ai_text стадии design). */
  review: string;
}

export interface CanvasProps {
  apiRef: { current: DesignCanvasApi | null };
  /** Ранее сохранённая схема (GET whiteboard) — холст восстанавливает её. */
  initialState?: WhiteboardState | null;
}

// Сессии, для которых холст уже сохранялся в этой вкладке: восстановление схемы
// (GET whiteboard) запрашиваем только при ВОЗВРАТЕ на стадию design (когда схема
// была сохранена), чтобы не делать лишнего GET на первом (пустом) входе.
const savedDesignSessions = new Set<number>();

export function DesignPanel({
  sessionId,
  canvas,
  review,
}: DesignPanelProps) {
  const Canvas = canvas ?? ExcalidrawCanvas;
  const apiRef = useRef<DesignCanvasApi | null>(null);
  const [placed, setPlaced] = useState<string[]>([]);
  const [submitting, setSubmitting] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [initialState, setInitialState] = useState<WhiteboardState | null>(null);
  // «ИИ оценивает…» — от успешного сохранения до появления нового review.
  const [evaluating, setEvaluating] = useState(false);
  const reviewAtSaveRef = useRef('');

  // Восстановление ранее сохранённой схемы (GET whiteboard → холст) при возврате
  // на стадию. Смысл только если схема уже сохранялась в этой вкладке; иначе холст
  // пуст (первый вход) и лишний GET не нужен. 404/ошибка — холст остаётся пустым.
  useEffect(() => {
    if (!savedDesignSessions.has(sessionId)) return;
    let alive = true;
    getWhiteboard(sessionId)
      .then((wb) => {
        if (!alive) return;
        if (wb.state && Array.isArray(wb.state.elements) && wb.state.elements.length > 0) {
          setInitialState(wb.state);
          if (wb.structure && Array.isArray(wb.structure.blocks) && wb.structure.blocks.length > 0) {
            setPlaced((prev) => (prev.length === 0 ? [...wb.structure!.blocks] : prev));
          }
        }
      })
      .catch(() => {
        /* нет сохранённой схемы (404) или сбой чтения — холст пуст, ошибка не показываем */
      });
    return () => {
      alive = false;
    };
  }, [sessionId]);

  // spec-14/D5: холст Excalidraw лениво грузится — раньше placeBlock при
  // apiRef.current === null молча ничего не рисовал, а счётчик «Блоков»
  // рос (можно было «Оценить» пустой холст). Теперь клик при неготовом
  // холсте ставит блок В ОЧЕРЕДЬ — он будет поставлен автоматически, как
  // только Excalidraw смонтируется (apiRef заполняется в его useEffect).
  const [canvasReady, setCanvasReady] = useState(false);
  const [canvasHint, setCanvasHint] = useState<string | null>(null);
  const pendingBlockRef = useRef<string | null>(null);
  useEffect(() => {
    const t = window.setInterval(() => {
      const ready = apiRef.current !== null;
      setCanvasReady(ready);
      const pending = pendingBlockRef.current;
      if (ready && pending !== null) {
        pendingBlockRef.current = null;
        apiRef.current?.placeBlock(pending);
        setPlaced((prev) => [...prev, pending]);
        setSaved(false);
        setCanvasHint(null);
      }
      if (ready) window.clearInterval(t);
    }, 300);
    return () => window.clearInterval(t);
  }, []);
  const placeBlock = useCallback((name: string) => {
    if (apiRef.current === null) {
      pendingBlockRef.current = name;
      setCanvasHint('Холст загружается — блок будет поставлен автоматически.');
      return;
    }
    setCanvasHint(null);
    apiRef.current.placeBlock(name);
    setPlaced((prev) => [...prev, name]);
    setSaved(false);
  }, []);

  const onClear = useCallback(() => {
    apiRef.current?.clear?.();
    pendingBlockRef.current = null;
    setCanvasHint(null);
    setPlaced([]);
    setSaved(false);
    setEvaluating(false);
  }, []);

  // Ревью пришло (review изменился после сохранения) — «оцениваем» заканчивается.
  useEffect(() => {
    if (evaluating && review !== reviewAtSaveRef.current) {
      setEvaluating(false);
    }
  }, [review, evaluating]);

  const onEvaluate = useCallback(async () => {
    if (submitting || apiRef.current === null) return;
    setSubmitting(true);
    setError(null);
    try {
      const snap = await apiRef.current.snapshot();
      await saveWhiteboard(sessionId, snap.state, { blocks: placed, links: snap.arrows }, snap.pngB64 ?? undefined);
      savedDesignSessions.add(sessionId); // при возврате на стадию схема восстановится
      setSaved(true);
      reviewAtSaveRef.current = review;
      setEvaluating(true);
    } catch (e) {
      if (e instanceof ApiError) {
        setError(`Схема: ${e.message}`);
      } else {
        setError(apiErrorMessage(e));
      }
    } finally {
      setSubmitting(false);
    }
  }, [submitting, sessionId, placed, review]);

  return (
    <div className="design" data-testid="design-panel">
      <p className="design-hint" data-testid="design-hint">
        1. Поставьте блоки из палитры. 2. Соедините их стрелками (инструмент «Стрелка»
        на панели слева: клик по блоку → клик по следующему): клиент → балансировка →
        сервисы → БД/кэш/очереди, мониторинг. 3. «Оценить схему» сохранит холст и
        попросит ИИ-ревью по рубрике: покрытие, масштабируемость, отказоустойчивость,
        trade-offs.
      </p>
      <div className="palette" aria-label="Палитра блоков">
        {DESIGN_BLOCK_GROUPS.map((group) => (
          <div className="palette-group" key={group.title}>
            <span className="palette-group__title">{group.title}</span>
            {group.names.map((name) => (
              <button
                key={name}
                type="button"
                className="palette-item"
                onClick={() => placeBlock(name)}
                data-testid={`block-${name.toLowerCase()}`}
                title={canvasReady ? undefined : 'Холст загружается — блок будет поставлен автоматически'}
              >
                <span
                  className="palette-item__dot"
                  style={{ background: blockColor(name) }}
                  aria-hidden="true"
                />
                {name}
              </button>
            ))}
          </div>
        ))}
      </div>

      <Canvas apiRef={apiRef} initialState={initialState} />
      {canvasHint !== null && (
        <p className="form-error" role="status" data-testid="design-canvas-hint">
          {canvasHint}
        </p>
      )}

      <div className="voice-controls">
        <button
          type="button"
          className="btn primary"
          onClick={() => void onEvaluate()}
          data-testid="design-evaluate"
          disabled={submitting || placed.length === 0}
        >
          {submitting ? 'Сохраняем…' : saved ? 'Схема сохранена — ждём оценку' : 'Оценить схему'}
        </button>
        <button
          type="button"
          className="btn ghost"
          onClick={onClear}
          disabled={placed.length === 0}
          title="Убрать все блоки со холста"
        >
          Очистить холст
        </button>
        {placed.length > 0 && (
          <span className="design-count">Блоков на холсте: {placed.length}</span>
        )}
        {evaluating && (
          <span className="design-evaluating" role="status">
            <span className="design-evaluating__spinner" aria-hidden="true" />
            ИИ оценивает схему (до ~1 минуты)
          </span>
        )}
        {error !== null && (
          <span className="form-error" role="alert" data-testid="design-error">{error}</span>
        )}
      </div>

      {review !== '' && (
        <div className="ai-review" data-testid="design-review">
          <strong>Оценка ИИ</strong>
          <p>{review}</p>
        </div>
      )}
    </div>
  );
}
