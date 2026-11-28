/**
 * Admin-UI (итерация 11): редактирование ВСЕХ шаблонов проекта.
 * Вкладки — Шаблоны (программы интервью) / Начала разговора / Голоса /
 * Роли собеседований; каждая — свой файл в src/views/admin/, переключение
 * вкладок — admin/tabs.tsx. Только is_admin (роут #/admin; доступ виден
 * только админу). Дизайн — стили проекта.
 */
import { useAuth } from '../auth';
import { useHashQuery } from '../hooks/useHashQuery';
import { ThemeToggle } from '../ThemeToggle';
import { ADMIN_TABS, AdminTabs, type AdminTab } from './admin/tabs';

export function AdminView() {
  const { user, logout } = useAuth();
  // Вкладка — в URL (#/admin?tab=voices): F5 и «Назад» сохраняют раздел
  // (ui-review 2026-11-27).
  const [query, setQuery] = useHashQuery();
  const tab: AdminTab = (ADMIN_TABS as readonly string[]).includes(query.get('tab') ?? '')
    ? (query.get('tab') as AdminTab)
    : 'templates';
  const setTab = (t: AdminTab): void => setQuery({ tab: t === 'templates' ? '' : t });

  // Защита: прямой заход на #/admin без прав — запрет (роут публичный, доступ по is_admin).
  if (user !== null && user.is_admin === false) {
    return (
      <main className="app">
        <h1>Админ</h1>
        <p className="form-error">Доступ запрещён: нужны права администратора.</p>
      </main>
    );
  }

  return (
    <main className="app cabinet">
      <header className="header">
        <div>
          <h1>Админ</h1>
          <p className="muted small">{user?.email}</p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <a className="btn ghost" href="#/">
            В кабинет
          </a>
          <ThemeToggle />
          <button type="button" className="btn ghost" onClick={logout}>
            Выйти
          </button>
        </div>
      </header>

      <AdminTabs tab={tab} onSelect={setTab} />
    </main>
  );
}
