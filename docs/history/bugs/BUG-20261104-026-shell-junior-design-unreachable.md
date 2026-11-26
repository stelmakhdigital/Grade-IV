# BUG-20261104-026: junior на shell-треке застревал на livecode — design недостижим

**Дата:** 2026-11-04
**Серьёзность:** P0 (блокер сценария)
**Статус:** closed (fix: SessionView navigates from track stage program)

## Симптомы
Junior на треке «Системный администратор» (shell): после практики (livecode)
нет кнопки «К System Design» — стадия design недостижима из UI, единственный
выход — «Завершить интервью». У middle/senior/staff на том же треке кнопка есть.

## Причина
`SessionView.tsx` строил навигацию по грейду сессии, а не по программе трека:

```tsx
{session.grade !== 'junior' && ( … К System Design … )}
```

Правило верно для кодовых треков (junior: voice → livecode → report), но
shell-треки имеют design на всех грейдах (spec-19): junior — voice → livecode
(bash) → design → report. Хардкод грейда разошёлся с данными трека.

## Исправление
- `trackStageMeta` (session engine, stage-сообщение) дополнен полем `stages` —
  программой стадий из `t.Stages(g)` (`models.Track.StageMeta`, единый builder).
- `SessionView.tsx`: кнопка «К System Design» видна, когда в `trackMeta.stages`
  design идёт после livecode; для легаси-сессий без `stages` — прежнее
  грейд-правило. Кнопка «Назад» с design на livecode — тоже для shell-треков.
- `ws.ts`: `TrackStageMeta.stages?: string[]`.

## Проверка
E2E (Playwright, junior, sysadmin): кнопка `[data-testid=go-design]` на практике
присутствует, переход на design работает; заголовок стадии «Практика (bash)».
