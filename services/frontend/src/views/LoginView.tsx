/**
 * Вход и регистрация (WP-7). Один экран, переключение режима;
 * ошибки API — по коду (email_exists, invalid_credentials, weak_password).
 */
import { useState, type FormEvent } from 'react';
import { ApiError } from '../api';
import { useAuth } from '../auth';

type Mode = 'login' | 'register';

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export function LoginView() {
  const { login, register } = useAuth();
  const [mode, setMode] = useState<Mode>('login');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const switchMode = (m: Mode) => {
    setMode(m);
    setError(null);
  };

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy) return;
    if (!EMAIL_RE.test(email)) {
      setError('Некорректный email');
      return;
    }
    if (password.length < 8) {
      setError('Пароль — не короче 8 символов');
      return;
    }
    setBusy(true);
    setError(null);
    try {
      if (mode === 'login') {
        await login(email, password);
      } else {
        await register(email, password);
      }
    } catch (err) {
      setError(apiErrorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <main className="app auth-page">
      <h1>Грейд</h1>
      <p className="muted">ИИ мок-интервью для разработчиков</p>
      <form className="card auth-card" onSubmit={onSubmit} noValidate>
        <div className="tabs" role="tablist">
          <button
            type="button"
            role="tab"
            aria-selected={mode === 'login'}
            className={mode === 'login' ? 'tab active' : 'tab'}
            onClick={() => switchMode('login')}
          >
            Вход
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={mode === 'register'}
            className={mode === 'register' ? 'tab active' : 'tab'}
            onClick={() => switchMode('register')}
          >
            Регистрация
          </button>
        </div>
        <label className="field">
          <span>Email</span>
          <input
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            autoComplete="email"
            required
          />
        </label>
        <label className="field">
          <span>Пароль</span>
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
            required
            minLength={8}
          />
        </label>
        {error !== null && (
          <p className="form-error" role="alert">
            {error}
          </p>
        )}
        <button type="submit" className="btn primary" disabled={busy}>
          {busy ? '…' : mode === 'login' ? 'Войти' : 'Создать аккаунт'}
        </button>
        <p className="muted small">
          Новым пользователям — 60 минут интервью бесплатно.
        </p>
      </form>
    </main>
  );
}

/** Человекочитаемое сообщение об ошибке по кодам API. */
export function apiErrorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    switch (err.code) {
      case 'email_exists':
        return 'Этот email уже зарегистрирован — войдите.';
      case 'unauthorized':
        return 'Неверный email или пароль.';
      case 'weak_password':
        return 'Пароль — не короче 8 символов.';
      case 'bad_email':
        return 'Некорректный формат email.';
      case 'network':
        return 'Нет соединения с сервером — проверьте, запущен ли api.';
      case 'no_minutes':
        return 'Недостаточно минут — пополните баланс.';
      default:
        return err.message || 'Ошибка запроса.';
    }
  }
  return err instanceof Error ? err.message : 'Непредвиденная ошибка.';
}
