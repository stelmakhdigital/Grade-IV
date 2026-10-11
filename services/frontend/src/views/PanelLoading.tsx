/**
 * Skeleton/spinner на время загрузки ленивого чанка тяжёлой панели (B2):
 * Live-Code (Monaco-обёртка), System Design (Excalidraw) и отчёт грузятся
 * лениво (React.lazy) — пока их чанк скачивается, Suspense показывает это.
 */
export function PanelLoading() {
  return (
    <div
      className="panel-loading"
      role="status"
      aria-live="polite"
      data-testid="panel-loading"
    >
      <span className="panel-loading__spinner" aria-hidden="true" />
      <span className="muted">Загрузка…</span>
    </div>
  );
}
