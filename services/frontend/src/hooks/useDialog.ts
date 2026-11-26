/**
 * Доступность модального диалога (ui-review 2026-11-24): Escape закрывает
 * независимо от того, куда ушёл фокус; Tab/Shift+Tab ходят по элементам внутри
 * диалога; при открытии фокус попадает внутрь; после закрытия возвращается на
 * кнопку, которая открыла диалог.
 *
 * Фон страницы на время открытия становится inert + aria-hidden: скринридер
 * его не озвучивает, клавиатура туда не уходит.
 */
import { useEffect, type MutableRefObject, type RefObject } from 'react';

const FOCUSABLE =
  'button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])';

/**
 * Пометить всё, кроме пути к диалогу. Обёртки-оверлеи рендерятся соседями к
 * <header> и секциям, поэтому обход по соседям вверх закрывает весь фон.
 * Возвращает функцию снятия пометок.
 */
function inertBackground(box: HTMLElement): () => void {
  const marked: Element[] = [];
  for (
    let node: HTMLElement | null = box;
    node !== null && node.parentElement !== null;
    node = node.parentElement
  ) {
    for (const sib of Array.from(node.parentElement.children)) {
      if (sib === node || sib.hasAttribute('inert')) continue;
      sib.setAttribute('inert', '');
      sib.setAttribute('aria-hidden', 'true');
      marked.push(sib);
    }
  }
  return () => {
    for (const el of marked) {
      el.removeAttribute('inert');
      el.removeAttribute('aria-hidden');
    }
  };
}

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
    // фон — вне диалога: inert снимается в cleanup этого же эффекта
    const unInert = boxRef.current !== null ? inertBackground(boxRef.current) : () => {};
    // autoFocus уже мог поставить фокус внутрь; если нет — ведём туда сами
    const box = boxRef.current;
    if (box !== null && !box.contains(document.activeElement)) {
      box.querySelector<HTMLElement>(FOCUSABLE)?.focus();
    }
    return () => {
      document.removeEventListener('keydown', onKey, true);
      unInert();
      // возврат фокуса — в cleanup, когда диалог закрылся (open true → false)
      returnFocusTo.current?.focus();
      returnFocusTo.current = null;
    };
  }, [open, onClose, boxRef, returnFocusTo]);
}
