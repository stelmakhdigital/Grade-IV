/**
 * Корень приложения (WP-7): hash-роутинг без зависимостей.
 *   #/            — кабинет (если нет авторизации — вход/регистрация)
 *   #/sessions/:id — страница сессии: голосовая (WS, WP-8) / запись (история)
 */
import { lazy, Suspense, useEffect, useState } from 'react';
import { AuthProvider, useAuth } from './auth';
import { CabinetView } from './views/CabinetView';
import { LoginView } from './views/LoginView';
import { SessionView } from './views/SessionView';
import { AdminView } from './views/AdminView';

// Публичная страница отчёта — ленивая (report-чанк с ReportBody не попадает
// в критический путь кабинета, code-splitting как у остальных вьюх).
const ShareReportView = lazy(() =>
  import('./views/report/ShareReportView').then((m) => ({ default: m.ShareReportView })),
);

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

// #/share/{token} — публичный отчёт (F1-export, spec-16): рендерится БЕЗ auth
// (гостевой роут), токен — ключ доступа к отчёту.
function parseShareToken(hash: string): string | null {
  const m = /^#\/share\/([0-9a-f]{16,})$/i.exec(hash);
  return m !== null ? m[1] : null;
}

// #/admin — admin-UI (итерация 11): редактирование шаблонов/начал/голосов.
function isAdminRoute(hash: string): boolean {
  return hash === '#/admin' || hash === '#/admin/';
}

function Routes() {
  const { status } = useAuth();
  const hash = useHashRoute();
  const sessionId = parseSessionId(hash);
  const shareToken = parseShareToken(hash);

  if (status === 'loading') {
    return (
      <main className="app">
        <h1>Грейд</h1>
        <p className="muted">Загрузка…</p>
      </main>
    );
  }
  // Публичная ссылка — раньше auth-gate перехватывал её и показывал логин.
  if (shareToken !== null) {
    return (
      <Suspense fallback={<main className="app"><h1>Грейд</h1><p className="muted">Загрузка…</p></main>}>
        <ShareReportView token={shareToken} />
      </Suspense>
    );
  }
  if (status === 'guest') {
    return <LoginView />;
  }
  if (isAdminRoute(hash)) {
    return <AdminView />;
  }
  if (sessionId !== null) {
    return <SessionView id={sessionId} />;
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
