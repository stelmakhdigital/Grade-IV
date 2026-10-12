# BUG-20261011-017 — System Design: артефакты прорисовки тулбара (CSS Excalidraw не импортирован)

**Статус:** исправлено (коммит с этим документом)

## Симптом

На тулбаре System Design:
1. Подписи горячих клавиш (1–8) не в уголках иконок, а вылетают
   отдельной большой колонкой (16px, `position: static`).
2. Между иконками видны серые «точки» — radio-инпуты внутри ToolIcon.
3. Иконки неквадратные (21×44 вместо 36×36).

Скриншот: `screenshots/bugs/bug-017-design-toolbar-css.png`
(до: колонка цифр + точки; после: нормальный тулбар).

## Шаги воспроизведения

1. Открыть System Design (любая активная сессия middle+).
2. Посмотреть на левый тулбар инструментов.

## Корневая причина

CSS библиотеки Excalidraw **нигде не импортировался** — только JS
(`import('@excalidraw/excalidraw')`). Excalidraw 0.18 требует отдельного
`import '@excalidraw/excalidraw/index.css'`. Без него стили тулбара
(`.ToolIcon__keybinding { position:absolute; font-size:.625rem }`, скрытие
`input.ToolIcon_type_radio`, квадратные иконки) не применяются —
включаются browser defaults. Холст при этом «работал», т.к. основная
расстановка — inline-стилями из JS.

## Фикс

`ExcalidrawCanvas.tsx`: `import '@excalidraw/excalidraw/index.css'` —
в дизайн-чанке (ленивый модуль), CSS не попадает в критический путь.

Проверено (headless Chromium): keybinding — 10px absolute, radio —
`opacity:0/absolute`, иконки 36×36; скриншот до/после — выше.
Тесты: vitest 114 passed, tsc чистый.
