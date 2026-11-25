/**
 * Подтверждение разрушающего действия (удалить шаблон, удалить трек) вместо
 * window.confirm. Браузерный confirm не наследует тему и фокус-ловушку,
 * блокирует страницу, не участвует в тестах и молча режется в iframe с
 * sandbox. Механика та же, что у модалок кабинета: useDialog (Escape, Tab
 * внутри диалога, возврат фокуса на кнопку, которая открыла диалог).
 */
import { useRef } from 'react';
import { useDialog } from './hooks/useDialog';

export interface AskConfirm {
  question: string;
  run: () => void;
}

export function ConfirmDialog(props: {
  ask: AskConfirm;
  confirmLabel?: string;
  onCancel: () => void;
}) {
  const boxRef = useRef<HTMLDivElement | null>(null);
  // Элемент, с которого убежал фокус (кнопка «Удалить»), — вернуть на него.
  const trigger = useRef(document.activeElement as HTMLElement | null);
  useDialog({ open: true, onClose: props.onCancel, boxRef, returnFocusTo: trigger });

  return (
    <div
      className="modal-overlay"
      onClick={(e) => {
        if (e.target === e.currentTarget) props.onCancel();
      }}
    >
      <div
        ref={boxRef}
        className="modal-box"
        role="dialog"
        aria-modal="true"
        aria-labelledby="confirm-title"
      >
        <div className="modal-head">
          <h3 id="confirm-title">Подтверждение</h3>
          <button type="button" className="btn ghost small" onClick={props.onCancel} aria-label="Закрыть">
            ✕
          </button>
        </div>
        <p className="modal-desc">{props.ask.question}</p>
        <div className="modal-actions">
          <button type="button" className="btn ghost" onClick={props.onCancel}>
            Отмена
          </button>
          <button
            type="button"
            className="btn danger"
            onClick={() => {
              const run = props.ask.run;
              props.onCancel();
              run();
            }}
            data-testid="confirm-yes"
          >
            {props.confirmLabel ?? 'Удалить'}
          </button>
        </div>
      </div>
    </div>
  );
}
