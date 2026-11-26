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

/** Путь роута без query (#/sessions/7?t=2 -> #/sessions/7). */
function pathOf(hash: string): string {
  const i = hash.indexOf('?');
  return i < 0 ? hash : hash.slice(0, i);
}

/** #/sessions/42 -> 42; null — если id не число. */
function parseSessionId(hash: string): number | null {
  const m = /^#\/sessions\/(\d+)$/.exec(pathOf(hash));
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
  const p = pathOf(hash);
  return p === '#/admin' || p === '#/admin/';
}

function Routes() {
  const { status, user } = useAuth();
  const hash = useHashRoute();
  const sessionId = parseSessionId(hash);
  const shareToken = parseShareToken(hash);
  const admin = isAdminRoute(hash);

  // Вкладка браузера должна различать экраны (ui-review 2026-11-27).
  useEffect(() => {
    document.title =
      shareToken !== null
        ? 'Отчёт — Грейд'
        : sessionId !== null
          ? `Интервью #${sessionId} — Грейд`
          : admin
            ? 'Админ — Грейд'
            : status === 'guest'
              ? 'Вход — Грейд'
              : 'Грейд';
  }, [hash, status, sessionId, shareToken, admin]);

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
  if (admin) {
    // Не-админ, пришедший по прямой ссылке, должен видеть «нет прав», а не
    // обвес админки с текстом 403 (ui-review 2026-11-27).
    if (user?.is_admin !== true) {
      return (
        <main className="app">
          <h1>Грейд</h1>
          <p className="muted">
            Недостаточно прав: эта страница только для администраторов.{' '}
            <a href="#/">В кабинет</a>
          </p>
        </main>
      );
    }
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
