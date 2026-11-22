# BUG-20261122-032: пустой task_id в /runs давал passed:true и балл 4 в отчёте

**Дата:** 2026-11-22
**Серьёзность:** P1 (переоценка практики без решения задачи)
**Статус:** closed (fix: проверка task_id в runs_handlers.go + фильтр в report.go)
**Issue:** stelmakhdigital/Grade-IV#8

## Симптомы
`POST /api/v1/sessions/{id}/runs` принимал любой `task_id`, включая пустой.
Без `task_id` sandbox не подставлял тесты банка (`sandbox main.go`: подстановка
только при непустом `task_id`), исполнялись только файлы кандидата, и
`go test ./...` по пакету без тестовых файлов («no test files», exit 0)
давал `passed: true, tests: []`:

```
curl -X POST .../sessions/<id>/runs -d '{"files":{"solution.go":"package main\n"},"action":"test","task_id":""}'
→ {"exit_code":0,"passed":true,"tests":[]}
```

Это событие `code_run` уходило в отчёт: детерминированный балл Live-Code — 4
из 5 (`heuristicReport`: любой `passed:true` → 4), в per-task списке фальшивка
не видна (`liveCodeTaskResults` пропускает пустой `task_id` — рядом `no_submission`),
в промпт ИИ-оценщика `mat.CodeRuns` подмешивался целиком. Чужой `task_id`
(задача другого грейда/стека) тоже не сверялся — кандидат мог гонять чужие
задачи с их тестами.

## Причина
`handleSessionRuns` проверял только `files`/`action`/стадию; сверки `task_id`
с задачами, назначенными сессии, не было нигде. Отчёт доверял любому событию
`code_run` с `passed:true`.

## Исправление
- `httpapi/runs_handlers.go`: после проверки стадии и `files` — `task_id`
  обязателен (пустой → 400 `invalid_request`) и должен быть среди задач,
  назначенных сессии (событие `livecode_tasks`, пишется `enterLiveCode`;
  чужой → 403 `task_not_assigned`). Отклонённый прогон не доходит ни до
  sandbox, ни до `submissions`, ни до события `code_run` — в отчёт ничего не
  уходит. Новый helper `sessionHasTask` (union по всем событиям `livecode_tasks` —
  повторные входы на стадию).
- `interviewer/report.go`: новый `assignedCodeRuns` — прогоны только с `task_id`
  из `livecode_tasks` сессии. Его используют и детерминированный балл
  (`heuristicReport`), и «Кодовые запуски» в промпте ИИ-оценщика
  (`GenerateReport`). Per-task список (`liveCodeTaskResults`) не менялся —
  пустые/чужие `task_id` в нём не видны изначально.
- Sandbox не трогался (issue #9 чинит его отдельно). Замечание: sandbox обязан
  считать пустой `task_id` ошибкой `400`, а не « прогоном без тестов» — «прогон
  без тестов → passed» само по себе неверный контракт; после issue #9 проверка
  api становится защитой в глубину, а не единственной.

## Проверка
`go vet ./... && go test ./...` — зелёные (api). Новые тесты (падают до фикса):
- `httpapi TestRunsTaskIDGate`: пустой `task_id` → 400, чужой → 403, запросов
  в sandbox/`submissions`/событий `code_run` ноль; назначенный `task_id` → 200.
- `interviewer TestHeuristicReportTaskIDGate`: `passed:true` с пустым/чужим
  `task_id` — балл 2 (не 4); с назначенным — 4.
- Затронутые фикстуры приведены к новому контракту (`TestRunsProxy`,
  `TestRunsSandboxDown`, `TestReportLifecycle` — прогоны с назначенным `task_id`).
