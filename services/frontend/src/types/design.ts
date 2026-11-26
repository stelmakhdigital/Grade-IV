/** System Design: состояние холста и структура схемы для оценки (ADR-004). */

/** Структура схемы System Design для оценки ИИ (ADR-004). */
export interface DesignStructure {
  blocks: string[];
  links: number;
}

/** Состояние холста Excalidraw (elements + appState) для PUT/GET whiteboard. */
export interface WhiteboardState {
  elements: unknown[];
  appState: unknown;
}
