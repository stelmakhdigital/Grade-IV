/**
 * MicVisualizer — мини-эквалайзер микрофона (14 полосок) в панели сессии.
 * Показывает, что микрофон жив: полосы «дышат» от уровня (onLevel worklet),
 * затухают в тишине. Режимы:
 *  - live — микрофон идёт (бирюза);
 *  - muted — ИИ говорит, микрофон подавлен (янтарный) — «сейчас не отвечайте»;
 *  - idle — микрофон выключен (тусклый).
 * Анимация — requestAnimationFrame с прямой постановкой высоты (без
 * re-render React на каждый такт); уровень приходит в levelRef.
 */
import { useEffect, useRef } from 'react';

export type MicEqMode = 'live' | 'muted' | 'idle';

const BARS = 14;

export function MicVisualizer({ mode, levelRef }: { mode: MicEqMode; levelRef: { current: number } }) {
  const rootRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const root = rootRef.current;
    if (root === null) return;
    const bars = Array.from(root.querySelectorAll<HTMLElement>('[data-bar]'));
    let raf = 0;
    let last = performance.now();
    let energy = 0; // сглаженный уровень 0..1

    const tick = (now: number) => {
      const dt = Math.min(0.1, (now - last) / 1000);
      last = now;
      // Новое значение уровня + экспоненциальное затухание (пад быстрее подъёма).
      const target = mode === 'live' ? Math.min(1, levelRef.current * 2.2) : 0;
      energy += (target - energy) * (target > energy ? Math.min(1, dt * 18) : Math.min(1, dt * 7));
      const t = now / 1000;
      for (let i = 0; i < bars.length; i++) {
        // «Эквалайзер»: каждая полоска со своей фазой/скоростью;
        // в режиме idle — ровный минимальный уровень.
        const wobble = mode === 'idle'
          ? 0.12
          : 0.25 + 0.75 * Math.abs(Math.sin(t * (1.7 + (i % 5) * 0.35) + i * 0.9));
        const h = mode === 'idle' ? 2 : Math.max(2, Math.round(energy * wobble * 26));
        bars[i].style.height = `${h}px`;
      }
      raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  }, [mode, levelRef]);

  return (
    <div
      ref={rootRef}
      className={`mic-eq ${mode}`}
      data-testid="mic-eq"
      role="img"
      aria-label={mode === 'live' ? 'микрофон активен' : mode === 'muted' ? 'микрофон заглушен: ИИ говорит' : 'микрофон выключен'}
    >
      {Array.from({ length: BARS }, (_, i) => (
        <span key={i} data-bar className="mic-eq-bar" />
      ))}
    </div>
  );
}
