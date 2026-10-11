/**
 * Панель стадии System Design (ADR-004, WP-10): палитра 12 архитектурных
 * блоков, холст (Excalidraw или инъектированный мок в тестах),
 * «Оценить» → PUT /whiteboard (state + структура) → ИИ-оценка (ai_text).
 */
import { useCallback, useEffect, useRef, useState } from 'react';
import type { ComponentType } from 'react';
import { ApiError, getWhiteboard, saveWhiteboard, type WhiteboardState } from '../../api';
import { apiErrorMessage } from '../LoginView';
import type { DesignCanvasApi } from './DesignCanvasApi';
import { ExcalidrawCanvas } from './ExcalidrawCanvas';

/** Палитра 12 блоков (ADR-004). */
export const DESIGN_BLOCKS = [
  'Client', 'Load Balancer', 'API-сервис', 'Worker', 'SQL-БД', 'NoSQL',
  'Кэш', 'Очередь', 'Message Broker', 'CDN', 'Внешний API', 'Monitoring',
] as const;

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

  const placeBlock = useCallback((name: string) => {
    apiRef.current?.placeBlock(name);
    setPlaced((prev) => [...prev, name]);
    setSaved(false);
  }, []);

  const onEvaluate = useCallback(async () => {
    if (submitting || apiRef.current === null) return;
    setSubmitting(true);
    setError(null);
    try {
      const snap = await apiRef.current.snapshot();
      await saveWhiteboard(sessionId, snap.state, { blocks: placed, links: snap.arrows }, snap.pngB64 ?? undefined);
      savedDesignSessions.add(sessionId); // при возврате на стадию схема восстановится
      setSaved(true);
    } catch (e) {
      if (e instanceof ApiError) {
        setError(`Схема: ${e.message}`);
      } else {
        setError(apiErrorMessage(e));
      }
    } finally {
      setSubmitting(false);
    }
  }, [submitting, sessionId, placed]);

  return (
    <div className="design" data-testid="design-panel">
      <p className="design-hint" data-testid="design-hint">
        Постройте архитектуру: поставьте блоки из палитры и соедините их стрелками
        (клиент → балансировка → сервисы → БД/кэш/очереди, мониторинг). «Оценить схему»
        сохранит холст и попросит ИИ-ревью по рубрике: покрытие, масштабируемость,
        отказоустойчивость, trade-offs.
      </p>
      <div className="palette" aria-label="Палитра блоков">
        {DESIGN_BLOCKS.map((name) => (
          <button
            key={name}
            type="button"
            className="palette-item"
            onClick={() => placeBlock(name)}
            data-testid={`block-${name.toLowerCase()}`}
          >
            {name}
          </button>
        ))}
      </div>

      <Canvas apiRef={apiRef} initialState={initialState} />

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
