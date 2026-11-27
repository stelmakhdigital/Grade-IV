/**
 * Детектор «молчащего» микрофона: RMS PCM-чанка, пороги тишины и машина
 * одноразовых событий mute/recover. Время передаётся аргументом — без DOM.
 */

// «Молчащий» микрофон: rms чанка < SILENCE_RMS непрерывно ≥ SILENCE_MS —
// устройство заглушено/отключено или вход на мьюте (ревью 2026-10-05: сервер
// видел rms 2→0 в сессии 13). Детектор чистый (время передаётся аргументом) — тесты
// без DOM.
export const SILENCE_RMS = 0.005;
export const SILENCE_MS = 5000;
// Тишина — норма в голосовом диалоге: пока ИИ говорит (свой TTS уходит в мик —
// эхо) и TTS_TAIL_GRACE_MS после его конца (кандидат думает/слушает) детектор
// «молчащего» микрофона не кормим. Инцидент 2026-10-09: ложная алерт «Микрофон
// молчит» в режиме записи на паузе 5 с (микросан нормальный).
export const TTS_TAIL_GRACE_MS = 30_000;

export function chunkRms(pcm: Int16Array): number {
  let s = 0;
  for (let i = 0; i < pcm.length; i++) {
    const v = pcm[i] / 32768;
    s += v * v;
  }
  return Math.sqrt(s / Math.max(1, pcm.length));
}

export interface SilenceEvents {
  /** Тишина ≥ SILENCE_MS (однажды за период). */
  onMuted: () => void;
  /** Уровень вернулся (rms ≥ SILENCE_RMS) после «молчания». */
  onRecover: () => void;
}

export class SilenceDetector {
  private silentSince: number | null = null;
  private muted = false;

  constructor(private readonly ev: SilenceEvents) {}

  reset(): void {
    this.silentSince = null;
    this.muted = false;
  }

  isMuted(): boolean {
    return this.muted;
  }

  feed(rms: number, now: number): void {
    if (rms >= SILENCE_RMS) {
      this.silentSince = null;
      if (this.muted) {
        this.muted = false;
        this.ev.onRecover();
      }
      return;
    }
    if (this.muted) return; // предупреждение за период — одно
    if (this.silentSince === null) {
      this.silentSince = now;
    } else if (now - this.silentSince >= SILENCE_MS) {
      this.muted = true;
      this.ev.onMuted();
    }
  }
}
