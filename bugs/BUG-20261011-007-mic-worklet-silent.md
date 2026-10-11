# BUG-20261011-007 · M1: AudioWorklet молчит, работает резервный захват

- **Статус:** исправлено
- **Область:** `services/frontend/src/audio/mic.ts`

## Симптом
В логах/UX: «Микрофон: основной путь (AudioWorklet) молчит, включён резервный захват»
(переход на ScriptProcessor). Основной (worklet) путь не выдаёт данные, работает fallback.

## Шаги
1. Включить микрофон в голосовой сессии.
2. `MicCapture.initCapture()`: `getUserMedia` → `new AudioContext()` (ПРЕСЛЕДУЕТСЯ
   `await getUserMedia`) → `audioWorklet.addModule` → `AudioWorkletNode`.
3. AudioContext, созданный после `await`, часто остаётся `suspended`
   (user-gesture-активация теряется/истекает).
4. **AudioWorkletNode в `suspended`-контексте НЕ вызывает `process`** → тишина →
   3 с без чанков → `switchToScriptProcessor` (fallback).

## Корень
`new AudioContext()` создавался без явного `resume()`. В средах, где активация жеста
потеряна после `await getUserMedia`, контекст стартует `suspended`; worklet-процессор в
suspended-контексте не расписывается (тишина), тогда как legacy ScriptProcessor в той же
среде даёт данные → ложный «основной путь молчит». (Объективная причина:
autoplay-политика браузера — context не запущен без user-gesture.)

## Фикс
Явный `ctx.resume()` (non-blocking, guard `typeof ctx.resume === 'function'`) сразу после
создания контекста (и в fallback-пути) — основной (worklet) путь стартует в `running`.
Если resume не сработает (нет активации) — watchdog (NO_DATA_MS) переключит на fallback
+ M4-реcovery. Переход на fallback остаётся прозрачным (onInfo + console.info, как было).

Регресс: FE vitest 114 зелёный (mock без resume — guard не ломает тесты).
