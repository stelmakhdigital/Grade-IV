# BUG-20261103-024 — docker-режим: --tmpfs монтируется noexec → «fork/exec …/task.test: permission denied»

**Обнаружен:** 2026-11-03, CI-джоба sandbox-docker, проба go-pass (лог — ветка `ci-debug`).
**Статус:** исправлен (services/sandbox/internal/runner/runner.go).
**Серьёзность:** блокер go-задач в docker-режиме.

## Симптом

Проба go-pass: тесты собираются, но не запускаются:

```
fork/exec /tmp/go-build3290074568/b001/task.test: permission denied
FAIL task 0.000s
```

Shell-задачи проходят (скрипты исполняются интерпретатором из образа,
исполнять файлы с /tmp не нужно) — потому docker-путь выглядел «полуживым».

## Причина

`docker run --tmpfs /tmp:size=128m` монтирует tmpfs с опциями по умолчанию
`rw,nosuid,noexec,...` — exec на /tmp запрещён. Go пишет тестовый бинарник
в TMPDIR (/tmp) и запускает его → permission denied.

## Фикс

`--tmpfs /tmp:size=128m,exec` — явно разрешён exec. Контейнер одноразовый,
isolated (`--network=none`, `--pids-limit`, `--memory`, `--cap-drop=ALL`) —
риск исполняемых файлов в /tmp внутри него ограничен тем же, что и любой
запуск кода кандидата.

## Регресс

`SANDBOX_DOCKER=1 go test -run Docker ./internal/runner/` — go-pass
(dial-тест с --network=none), py-pass, py-oom, shell, fork-bomb.
