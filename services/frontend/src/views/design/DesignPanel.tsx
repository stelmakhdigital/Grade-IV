/**
 * Панель стадии System Design (ADR-004, WP-10): палитра 12 архитектурных
 * блоков, холст (Excalidraw или инъектированный мок в тестах),
 * «Оценить» → PUT /whiteboard (state + структура) → ИИ-оценка (ai_text).
 */
import { useCallback, useRef, useState } from 'react';
import type { ComponentType } from 'react';
import { ApiError, saveWhiteboard } from '../../api';
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
}

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

      <Canvas apiRef={apiRef} />

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
