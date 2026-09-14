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
  /**
   * PNG схемы (base64, без префикса) для vision-оценки ИИ (ADR-004).
   * Опционально: холст без элементов — null (оценка по структуре).
   */
  pngB64?: string | null;
}

export interface DesignCanvasApi {
  /** Вставить блок палитры (группа: прямоугольник + подпись). */
  placeBlock: (name: string) => void;
  /** Текущее состояние холста для сохранения (PNG-экспорт — асинхронный). */
  snapshot: () => Promise<DesignSnapshot>;
}
