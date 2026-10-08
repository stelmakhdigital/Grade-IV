/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** Dev-диагностика микрофона (live-отчёты, строка статистики в UI). */
  readonly VITE_MIC_DEBUG?: string;
}
