# BUG-20261122-034: stage от finish без track-меты — UI терял роль и подпись «Практика (bash)»

**Дата:** 2026-11-22
**Серьёзность:** P2 (симптом виден до перезагрузки страницы; данные отчёта не
искажаются — портится только подпись раздела и шапка)
**Статус:** closed (fix: track в stage-сообщении от finish + мета не затирается)
**Связано с:** BUG-20261104-026, BUG-20261104-028 (тот же класс: stage-сообщение
без `track` затирает `trackMeta` клиента), `a4a2298` (добавил `track` в
`Transition`/`ResendStage`, про `finish` не вспомнил)

## Симптомы
Кандидат дошёл до практики и нажал «Завершить», не перезагружая страницу:

- в шапке «Интервью: middle» вместо «Интервью: Системный администратор»;
- в отчёте раздел практики подписан «Live-Code: задачи» вместо
  «Практика (bash): задачи».

После F5 всё правильно (мета догружается из справочника `GET /tracks` для
завершённых сессий), поэтому баг замечали редко — интервьюер смотрит отчёт
как раз в момент финала.

## Причина
Клиент ставит мету безусловно из stage-сообщения: `setTrackMeta(m.track ?? null)`
(`services/frontend/src/hooks/useVoiceSession.ts`). Отсутствие поля = `null`.

О переходе на `report` сообщает `finish`
(`services/api/internal/session/engine_lifecycle.go`) — он и не добавлял
`track`, в отличие от `Transition` и `ResendStage`. Любой путь на `report`
(кнопка «Завершить» и `stage_action: report`, который в `Transition`
делегирует сообщение в `finish`) уходил без меты.

## Исправление
- `session/engine_lifecycle.go` (`finish`): stage-сообщение о `report` получает
  `track` через `trackStageMeta(rt.track, rt.grade)` — как в `Transition`.
  `rt.track`/`rt.grade` после создания рантайма не меняются, чтение вне
  `connMu` безопасно (ровно так же делает `Transition`).
- `hooks/useVoiceSession.ts`: `setTrackMeta(m.track ?? trackMetaRef.current)` —
  сообщение без поля больше не затирает накопленную мету. Источник правды
  остаётся сервер (он кладёт `track` в каждое stage-сообщение), это только
  защита от следующей такой дыры.

## Проверка
- Новый go-тест `internal/session/engine_finish_track_test.go`: настоящее
  WS-соединение к движку (`httptest` + nhooyr), `Finish` → клиент читает
  `stage/report` и видит `track.title` и `track.practice.runtime = shell`.
  До фикса падал с `stage(report) без track-меты: map[name:report task:<nil>
  type:stage]`.
- Новый vitest-тест в `src/views/SessionView.test.tsx`: стадия пришла с метой
  shell-роли, затем `stage/report` БЕЗ `track` и закрытие WS → шапка держит
  имя роли, в отчёте «Практика (bash): задачи». До правки хука падал
  («Интервью: middle»).
- `go vet ./...` + `go test ./...` (services/api) — ok; `npx tsc --noEmit` ok,
  vitest 19 файлов / 141 passed.
