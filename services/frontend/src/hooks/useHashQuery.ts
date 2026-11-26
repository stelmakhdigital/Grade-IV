/**
 * Query-часть hash-роута: #/?f=active, #/admin?tab=tracks, #/sessions/7?t=2.
 * Нужна, чтобы F5 и «Назад» сохраняли выбор (фильтр, вкладка, номер задачи) —
 * раньше такое жило только в состоянии React и терялось (ui-review 2026-11-27).
 */
import { useCallback, useEffect, useState } from 'react';

function queryOf(hash: string): URLSearchParams {
  const i = hash.indexOf('?');
  return new URLSearchParams(i < 0 ? '' : hash.slice(i + 1));
}

/** Пустое значение ключа удаляет его из адреса. */
export function useHashQuery(): [URLSearchParams, (next: Record<string, string>) => void] {
  const [query, setQuery] = useState(() => queryOf(window.location.hash));
  useEffect(() => {
    const onChange = () => setQuery(queryOf(window.location.hash));
    window.addEventListener('hashchange', onChange);
    return () => window.removeEventListener('hashchange', onChange);
  }, []);

  const setQuery_ = useCallback((next: Record<string, string>) => {
    const path = window.location.hash.split('?')[0] || '#/';
    const p = new URLSearchParams();
    for (const [k, v] of Object.entries(next)) {
      if (v !== '') p.set(k, v);
    }
    const q = p.toString();
    window.location.hash = q === '' ? path : `${path}?${q}`;
  }, []);

  return [query, setQuery_];
}
