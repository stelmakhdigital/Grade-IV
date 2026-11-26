# BUG-20261103-023 — docker-режим: холодная сборка Go переполняет tmpfs /tmp (128m) → «no space left on device»

**Обнаружен:** 2026-11-03, CI-джоба sandbox-docker, проба go-pass (лог — ветка `ci-debug`).
**Статус:** исправлен (services/sandbox/internal/runner/runner.go).
**Серьёзность:** блокер docker-режима для go-задач (prod compose).

## Симптом

Проба go-pass падает на линковке тестового бинарника внутри контейнера:

```
/usr/local/go/pkg/tool/linux_amd64/link: mapping output file failed:
no space left on device
{"Action":"build-fail"}
```

Сборка маленькой задачи занимала >128 МБ кэша.

## Причина

Раннер (spec-14/C6) монтирует rootfs read-only и выставляет
`GOCACHE=/tmp/gocache` — /tmp это tmpfs размером 128m (`--tmpfs /tmp:size=128m`,
RAM-бэкед). Холодная сборка нет-стека (net + crypto + зависимостей) пишет
~150 МБ кэша — tmpfs переполняется. На старых сборках образа golang:1.24
кэш помещался (проба T-20261009133832 была зелёной); свежие образы
перестарались в размере нет-стека — плюс добавленный CGO-путь раздул кэш.

## Фикс

1. `GOCACHE=/work/.gocache` — кэш на bind-mount workdir (дисковый, не RAM),
   зачищается вместе с workdir после прогона (ос.RemoveAll).
2. `CGO_ENABLED=0` — сборка без gcc, кэш в 2–3 раза меньше; cgo-задачи и
   раньше не поддерживались (GOPROXY=off, слои образа без toolchain-гарантий).

## Регресс

`SANDBOX_DOCKER=1 go test -run Docker ./internal/runner/` — джоба
`sandbox-docker` в CI (go-pass с сетевым dial-тестом обязан собраться).
