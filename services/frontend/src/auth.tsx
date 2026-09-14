/**
 * AuthContext (WP-7): токен в localStorage, профиль и баланс минут.
 * При старте: если есть токен — /auth/me (валидность); 401 → разлогин.
 */
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react';
import {
  ApiError,
  login as apiLogin,
  me as apiMe,
  register as apiRegister,
  setToken,
  getToken,
  type MeResult,
  type User,
} from './api';

export type AuthStatus = 'loading' | 'guest' | 'authed';

export interface AuthContextValue {
  status: AuthStatus;
  user: User | null;
  minutesRemainingS: number;
  /** Минут (без дробной) для отображения. */
  minutesRemainingMin: number;
  login: (email: string, password: string) => Promise<void>;
  register: (email: string, password: string) => Promise<void>;
  logout: () => void;
  refresh: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<AuthStatus>('loading');
  const [user, setUser] = useState<User | null>(null);
  const [minutes, setMinutes] = useState(0);

  const applyMe = useCallback((r: MeResult) => {
    setUser(r.user);
    setMinutes(r.minutes_remaining_s);
    setStatus('authed');
  }, []);

  const refresh = useCallback(async () => {
    if (!getToken()) {
      setStatus('guest');
      setUser(null);
      return;
    }
    try {
      applyMe(await apiMe());
    } catch (e) {
      if (e instanceof ApiError && (e.status === 401 || e.status === 403)) {
        setToken(null);
        setUser(null);
        setStatus('guest');
      } else {
        // Сеть/сервер недоступны — показываем гостя, но не удаляем токен
        // (пользователь сможет вернуться после восстановления).
        setStatus('guest');
        setUser(null);
      }
    }
  }, [applyMe]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const login = useCallback(
    async (email: string, password: string) => {
      const r = await apiLogin(email, password);
      setToken(r.token);
      applyMe({ user: r.user, minutes_remaining_s: r.minutes_remaining_s });
    },
    [applyMe],
  );

  const register = useCallback(
    async (email: string, password: string) => {
      const r = await apiRegister(email, password);
      setToken(r.token);
      applyMe({ user: r.user, minutes_remaining_s: r.minutes_remaining_s });
    },
    [applyMe],
  );

  const logout = useCallback(() => {
    setToken(null);
    setUser(null);
    setMinutes(0);
    setStatus('guest');
  }, []);

  const value = useMemo<AuthContextValue>(
    () => ({
      status,
      user,
      minutesRemainingS: minutes,
      minutesRemainingMin: Math.floor(minutes / 60),
      login,
      register,
      logout,
      refresh,
    }),
    [status, user, minutes, login, register, logout, refresh],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const v = useContext(AuthContext);
  if (v === null) {
    throw new Error('useAuth вне AuthProvider');
  }
  return v;
}
