# BUG-20261011-001 · Стадия System Design недостижима из UI

- **Статус:** исправлено (коммит `a858001`)
- **Грейды:** middle / senior / staff (junior design не входит в программу — корректно)
- **Область:** `services/frontend/src/views/SessionView.tsx`

## Симптом
В реальной сессии на стадии **livecode** кандидат **не видит кнопку
«К System Design»** и не может перейти на стадию design (доска не открывается).
Переход `voice → livecode` работал; `livecode → design` — нет.

## Шаги воспроизведения
1. Создать сессию (грейд middle/senior/staff), войти, дойти до стадии **livecode**
   (кнопка «К Live-Code» из voice — работала).
2. На стадии livecode — в области управления **нет кнопки «К System Design»**.
3. Стадия design недостижима из UI (только backend-переход `stage_action`, который
   UI не генерирует; LLM-интервьюер сам на design не переводит).

## Корень
Кнопка перехода в design стояла внутри **else-ветки** тернарного выражения панелей
в `SessionView.tsx`, которое рендерится **только когда стадия НЕ livecode и НЕ design**
(т.е. в voice). Условие самой кнопки — `stage === 'livecode'`. Но в livecode
рендерится ветка `<LiveCodePanel>` (первая ветка тернарного), а else-ветка (с
кнопкой) — **нет**. Следовательно, кнопка с условием `stage === 'livecode'` не
рендерилась никогда: условие могло стать истинным только в состоянии, когда её
контейнер уже не в DOM. Классический «мёртвый» рендер: условие-фильтр и
контейнер-ветка взаимоисключающи.

Проверено по цепочке (backend-переход исправен, проблема чисто в UI):
- `engine.Transition` + `CanTransitionStage` (state.go): livecode→design разрешён
  для middle/senior/staff (junior — no), возврат design→livecode разрешён.
- `ws.go handleUIEvent` case `stage_action`: `Transition` + `OnStageChanged` (формат
  стадии, TTS). Работает — но UI это событие не отправлял (кнопки не было).

## Исправление
Перенёс навигацию между стадиями в **каждую ветку панелей**:
- livecode → «К System Design» (только если `session.grade !== 'junior'`);
- design → «К Live-Code (назад)» (возврат на пред. стадию).
Кнопка теперь рендерится в нужной стадии. + CSS `.stage-nav`.

Регресс: frontend 114 passed, api `go test` зелёные; тестовые файлы и CI не
менялись. Скриншоты ДО/ПОСЛЕ — `screenshots/design/`.
