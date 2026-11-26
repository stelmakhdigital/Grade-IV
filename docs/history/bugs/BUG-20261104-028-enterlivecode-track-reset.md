# BUG-20261104-028: второй stage-livecode без track-меты сбрасывал UI-навигацию

**Дата:** 2026-11-04
**Серьёзность:** P1 (ломает fix 026)
**Статус:** closed (fix: enterLiveCode шлёт track-мету)

## Симптомы
После входа на livecode заголовок сессии возвращался к сырому грейду
(«Интервью: junior»), подпись стадии — «Live-Code» вместо «Практика (bash)»,
кнопка «К System Design» исчезала (fix 026 не работал на практике).

## Причина
Клиент хранит trackMeta из последнего stage-сообщения
(`setTrackMeta(m.track ?? null)` в useVoiceSession). `enterLiveCode` отправлял
СВОЁ stage-сообщение с задачами — БЕЗ `track` — и оно затирало мету, полученную
на voice/переходе. WS-проба: три stage-сообщения — voice (track ok), transition
livecode (track ok), enterLiveCode (track: undefined).

## Исправление
`enterLiveCode` (ws.go) добавляет `track` (мета через `models.Track.StageMeta`
по треку сессии) в сообщение с задачами.

## Проверка
WS-проба: оба stage-livecode несут одинаковую мету с `stages`; заголовок и
подписи не сбрасываются.
