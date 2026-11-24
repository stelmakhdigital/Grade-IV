/**
 * Доступность модального диалога (ui-review 2026-11-24): Escape закрывает
 * независимо от того, куда ушёл фокус; Tab/Shift+Tab ходят по элементам внутри
 * диалога; при открытии фокус попадает внутрь; после закрытия возвращается на
 * кнопку, которая открыла диалог.
 *
 * shortcut: фон страницы скрыт от клавиатуры (ловушкой Tab), но не помечен
 * aria-hidden/inert — скринридер его по-прежнему озвучивает. Апгрейд — когда
 * диалогов станет больше двух-трёх: рендерить оверлей порталом в body и вешать
 * inert на контейнер приложения.
 */
import { useEffect, type MutableRefObject, type RefObject } from 'react';

const FOCUSABLE =
  'button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])';

export function useDialog(opts: {
  open: boolean;
  onClose: () => void;
  /** Контейнер диалога ([role=dialog]). */
  boxRef: RefObject<HTMLElement | null>;
  /** Элемент, на который вернуть фокус при закрытии (кнопка «Резюме»…). */
  returnFocusTo: MutableRefObject<HTMLElement | null>;
}): void {
  const { open, onClose, boxRef, returnFocusTo } = opts;

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        onClose();
        return;
      }
      if (e.key !== 'Tab') return;
      const box = boxRef.current;
      if (box === null) return;
      const items = Array.from(box.querySelectorAll<HTMLElement>(FOCUSABLE));
      if (items.length === 0) return;
      const first = items[0];
      const last = items[items.length - 1];
      const cur = document.activeElement;
      if (cur !== null && !box.contains(cur)) {
        e.preventDefault();
        (e.shiftKey ? last : first).focus();
      } else if (!e.shiftKey && cur === last) {
        e.preventDefault();
        first.focus();
      } else if (e.shiftKey && cur === first) {
        e.preventDefault();
        last.focus();
      }
    };
    document.addEventListener('keydown', onKey, true);
    // autoFocus уже мог поставить фокус внутрь; если нет — ведём туда сами
    const box = boxRef.current;
    if (box !== null && !box.contains(document.activeElement)) {
      box.querySelector<HTMLElement>(FOCUSABLE)?.focus();
    }
    return () => {
      document.removeEventListener('keydown', onKey, true);
      // возврат фокуса — в cleanup, когда диалог закрылся (open true → false)
      returnFocusTo.current?.focus();
      returnFocusTo.current = null;
    };
  }, [open, onClose, boxRef, returnFocusTo]);
}
