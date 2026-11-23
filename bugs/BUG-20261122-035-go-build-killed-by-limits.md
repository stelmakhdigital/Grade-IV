# BUG-20261122-035: сборка go под лимитами песочницы убивалась — go-задачи падали «build failed» на ровном месте

**Дата:** 2026-11-23
**Серьёзность:** P1 (нестабильная проверка go-задач: честное решение получает «build failed»)
**Статус:** closed (fix: `GOFLAGS=-mod=mod -p=2` в go-окружении обоих режимов)
**Связано с:** BUG-20261103-023 (GOCACHE в docker переехал с tmpfs на bind-mount),
BUG-20261122-033 (регресс `TestGoGraderTestsOverlaid` это и обнаружил).

## Симптомы

`TestGoGraderTestsOverlaid` падал примерно на каждом втором прогоне (`go test ./...`
в `services/sandbox`) на хосте с 28 ядрами:

```
--- FAIL: TestGoGraderTestsOverlaid (2.76s)
    честное решение не прошло тестами банка:
    {"ImportPath":"runtime","Action":"build-output","Output":"runtime: .../compile: signal: killed\n"}
    {"ImportPath":"runtime","Action":"build-fail"}
    FAIL    task [build failed]
```

Кандидат в этом случае получил бы `run_result` с «build failed» при полностью
корректном решении. То же относилось и к docker-режиму: `TestSandboxDockerProbe`
(go-ветка) на этом хосте падала ещё до правки изоляции.

## Причина

GOCACHE в обоих режимах — каталог внутри per-run workdir (`runner.go`,
`GOCACHE=<workdir>/.gocache`), то есть stdlib пересобирается с нуля на каждый
прогон. Go берёт параллельность сборки равной числу ядер (28), и каждый
`compile` занимает VA. В subprocess-режиме на стеке go стоит
`RLIMIT_AS = defaultMemLimitGo = 2 GiB`, в docker — `--memory=512m` плюс
`--pids-limit=128`. Пик 28 параллельных compile то влезал в потолок, то нет —
отсюда «плавающесть»: ядро убивало `compile` с `SIGKILL`, и сборка `runtime`
падала.

## Исправление

`GOFLAGS=-mod=mod -p=2` в go-окружении обоих режимов (`cleanEnv` для subprocess и
ENV для docker). `-p=2` режет и пик памяти сборки, и число процессов. Прогон
go-сборки стал медленнее (~8.5 с вместо ~5.9 с на cold cache), что для
docker-режима с `--cpus=1` не видно.

Альтернативы, от которых отказались: поднимать `defaultMemLimitGo` (VA-потолок
уезжает вверх, а сама проблема — в пересборке std каждый прогон), постоянный
GOCACHE mount (кэш между прогонами кандидата — отдельное решение и отдельная
граница: кэшем делятся последовательные сдачи).

## Проверка

```
for i in $(seq 8); do go test ./internal/runner/ -run TestGoGraderTestsOverlaid -count=1; done
```

8 из 8 зелёные (до правки — 3 из 5 на этом же хосте). Полный прогон
`go vet ./... && go test ./... -count=1` в `services/sandbox` — зелёный.
