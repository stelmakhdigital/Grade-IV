/**
 * Императивный API холста System Design (ADR-004, WP-10).
 * DesignPanel работает через него; реализация — Excalidraw
 * (ExcalidrawCanvas) или мок в тестах.
 */
export interface DesignSnapshot {
  /** JSON Excalidraw: elements + appState (состояние для PUT /whiteboard). */
  state: { elements: unknown[]; appState: unknown };
  /** Число стрелок (связей) на холсте. */
  arrows: number;
}

export interface DesignCanvasApi {
  /** Вставить блок палитры (группа: прямоугольник + подпись). */
  placeBlock: (name: string) => void;
  /** Текущее состояние холста для сохранения. */
  snapshot: () => DesignSnapshot;
}
