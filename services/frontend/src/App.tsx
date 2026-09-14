/**
 * Корень приложения (WP-7): hash-роутинг без зависимостей.
 *   #/            — кабинет (если нет авторизации — вход/регистрация)
 *   #/sessions/:id — страница сессии (метаданные + транскрипт;
 *                    голосовой интерфейс — WP-8)
 */
import { useEffect, useState } from 'react';
import { AuthProvider, useAuth } from './auth';
import { CabinetView } from './views/CabinetView';
import { LoginView } from './views/LoginView';
import { TranscriptView } from './views/TranscriptView';

function useHashRoute(): string {
  const [hash, setHash] = useState(() => window.location.hash || '#/');
  useEffect(() => {
    const onChange = () => setHash(window.location.hash || '#/');
    window.addEventListener('hashchange', onChange);
    return () => window.removeEventListener('hashchange', onChange);
  }, []);
  return hash;
}

/** #/sessions/42 -> 42; null — если id не число. */
function parseSessionId(hash: string): number | null {
  const m = /^#\/sessions\/(\d+)$/.exec(hash);
  return m !== null ? Number(m[1]) : null;
}

function Routes() {
  const { status } = useAuth();
  const hash = useHashRoute();
  const sessionId = parseSessionId(hash);

  if (status === 'loading') {
    return (
      <main className="app">
        <h1>Грейд</h1>
        <p className="muted">Загрузка…</p>
      </main>
    );
  }
  if (status === 'guest') {
    return <LoginView />;
  }
  if (sessionId !== null) {
    return <TranscriptView id={sessionId} />;
  }
  return <CabinetView />;
}

export function App() {
  return (
    <AuthProvider>
      <Routes />
    </AuthProvider>
  );
}
