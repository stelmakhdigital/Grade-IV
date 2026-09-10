import { useEffect, useState } from 'react';
import { apiHealth } from './api';

type ApiStatus = 'checking' | 'ok' | 'unavailable';

/**
 * Каркас приложения (WP-1). Кабинет кандидата — WP-7,
 * голосовая сессия — WP-8, Live-Code — WP-9, System Design — WP-10.
 */
export function App() {
  const [status, setStatus] = useState<ApiStatus>('checking');

  useEffect(() => {
    let alive = true;
    apiHealth().then((ok) => {
      if (alive) setStatus(ok ? 'ok' : 'unavailable');
    });
    return () => {
      alive = false;
    };
  }, []);

  const badgeClass = status === 'ok' ? 'badge ok' : status === 'unavailable' ? 'badge err' : 'badge';
  const badgeLabel =
    status === 'ok' ? 'api: доступен' : status === 'unavailable' ? 'api: недоступен' : 'api: проверка…';

  return (
    <main className="app">
      <h1>Грейд</h1>
      <p>ИИ мок-интервью для разработчиков. Каркас (WP-1); кабинет — WP-7.</p>
      <span className={badgeClass}>{badgeLabel}</span>
    </main>
  );
}
