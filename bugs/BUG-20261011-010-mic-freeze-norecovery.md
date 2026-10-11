# BUG-20261011-010 · M4: «Недостаточно речи» — накопление речи умирает в середине сессии

- **Статус:** исправлено
- **Область:** `services/frontend/src/audio/mic.ts`

## Симптом
В какой-то момент сессии пользователь говорит долго, но UI всегда «Недостаточно речи для
отправки (минимум ~1.5 с)»: счётчик длительности (`recTotalSpeechMs`) застревает.

## Корень
`recTotalSpeechMs` — с серверных `stt_segment.total_speech_ms`. Сегменты не приходят,
потому что **клиент перестаёт шить PCM** — захват замёрз. Причина заморозки: **AudioContext
уходит в `suspended` mid-session** (tab в фон / авто-suspend / джиттер) → worklet-`process`
перестаёт вызываться → чанков нет → серверу нет аудио → нет `stt_segment` → счётчик речи
не растёт.

Критично: no-data watchdog был **разовым** (3 с «был ли первый чанок» после старта) и
снимался первым чанком (`onFirstChunk`) — дальше **не перезапускался**. Mid-session freeze
(после первого чанка) НЕ детектировался → самовосстановления не было. (Связь с M1: тот же
suspend-контекст.)

## Фикс
Самовосстановление при «молчит при активном входе»:
- **`statechange`-listener на AudioContext** (guard: jsdom-мок без `addEventListener`):
  когда контекст уходит в `suspended`, пока мик `running` → `ctx.resume()` (если есть
  активация) + re-arm no-data watchdog (если resume не сработал — fallback/reinit за 3 с).
  Listener снимается в `disposeCapture` (без утечки).
- M1 `ctx.resume()` при старте (основной путь в `running` с самого начала).

Ограничение (зафиксировано): browser autoplay-политика не даёт `resume()` suspended-контекста
без user-gesture — полная recovery происходит при следующем жесте (возврат в tab/клик);
listener обеспечивает детект + best-effort resume + переключение на fallback/reinit.

Регресс: FE vitest 114 зелёный (mock без statechange — guard не ломает тесты; existing
«first chunk → no reinit» поведение сохранено — watchdog остаётся разовым по чанкам).
