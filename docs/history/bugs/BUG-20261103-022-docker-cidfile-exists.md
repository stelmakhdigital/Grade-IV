# BUG-20261103-022 — docker-режим: cidfile существует до `docker run` → exit 125 на каждом запуске

**Обнаружен:** 2026-11-03, CI-джоба sandbox-docker (первый реальный прогон docker-проб; лог — ветка `ci-debug`).
**Статус:** исправлен (services/sandbox/internal/runner/runner.go, commit после этого документа).
**Серьёзность:** блокер docker-режима (prod-деплой compose: `SANDBOX_MODE=docker`).

## Симптом

Каждый запуск задачи в docker-режиме падает мгновенно:

```
docker: container ID file found, make sure the other container isn't
running or delete /tmp/grade-sandbox-cid-2616856053
```

`ExitCode: 125`, `DurationMS: ~13`, тестов ноль. В subprocess-режиме не
воспроизводится (docker не нужен).

## Причина

Раннер готовил `--cidfile` так:

```go
cidFile, _ := os.CreateTemp("", "grade-sandbox-cid-*") // создаёт ПУСТОЙ файл
cidFile.Close()
// ... args = {"run", "--rm", ..., "--cidfile", cidPath, ...}
```

Docker-CLI ожидает, что cidfile НЕТ на диске — он создаёт его сам и пишет
туда ID контейнера (на это опирается и kill-логика spec-14/C4 по
`ctx.Done()`). Если файл уже существует — современный CLI (27+) отвечает
«container ID file found» и отказывает (exit 125).

**Почему раньше не видели:** пробы T-20261009133832 гонялись на хосте со
старым docker (20.x), где пустой существующий cidfile проходил; обещанная в
шапке ci.yml джоба sandbox-docker добавлена только сейчас (spec-20) — на
runner'ах с новым CLI баг и проявился.

## Фикс

Файл temp используется только как уникальное имя пути; сразу после создания
и закрытия он удаляется:

```go
if err := os.Remove(cidPath); err != nil { ... }
```

Дальше docker создаёт файл сам, kill-логика (docker kill по cidfile) не меняется.

## Регресс

`SANDBOX_DOCKER=1 go test -run Docker ./internal/runner/` — джоба
`sandbox-docker` в CI (go/python/shell пробы + форк-бомба за `--pids-limit`).
Лог падения при следующем сбое коммитится в ветку `ci-debug` (API логов
GitHub под auth, ветка публичного репо — читается анонимно).
