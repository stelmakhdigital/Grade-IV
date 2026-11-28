import './unload-guard'; // до всего остального: вешать unload браузеру уже нельзя
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { App } from './App';
import { applyTheme, currentTheme, watchSystemTheme } from './theme';
import './index.css';

// Тема до первого кадра: иначе светлая система на миг вспыхивает тёмным.
applyTheme(currentTheme());
watchSystemTheme();

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
