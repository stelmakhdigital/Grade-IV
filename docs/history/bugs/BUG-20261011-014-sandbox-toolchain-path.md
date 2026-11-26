# BUG-20261011-014 · Sandbox (subprocess): `go: not found` (exit 127) — тулчейн не в PATH

- **Статус:** исправлено (операционно + hardening)
- **Область:** `services/sandbox/cmd/sandbox/main.go`, окружение запуска `scripts/run-all.sh`

## Симптом
Все запуски Go-кода в Live-Code возвращают мгновенно (18–30 мс)
`exit_code=127`, `stdout: "sh: 1: go: not found"`, `tests: null` — даже на корректном
решении. Python аналогично, если `python3` не в PATH.

## Шаги воспроизведения
1. Запустить sandbox из окружения, где `go` недоступен в PATH процесса
   (например, shell агента без `~/.local/go-toolchain/bin`; run-all.sh, напротив,
   дообавляет его на строке 29).
2. Любой POST /runs со стеком go → `go: not found`.

## Корень
`runner.runSubprocess` честно выполняет `sh -c "go test -count=1 -json ./..."`
(и `python3 -m pytest -q`) в наследуемом PATH. При отсутствии тулчейна каждый запуск
падает с exit 127, но на старте сервиса **никто не проверял** наличие инструментов —
ошибка видна только в stdout каждого прогона.

## Фикс
1. **Операционно**: sandbox перезапущен с PATH, содержащим go-тулчейн
   (`~/.local/go-toolchain/bin`); прогон `go-fizzbuzz` с правильным решением →
   `exit 0, passed=true, tests=[TestFizzBuzz PASS]`.
2. **Hardening** (`cmd/sandbox/main.go`): на старте в subprocess-режиме
   `exec.LookPath` по `go` и `python3`:
   - нашёл → `INFO toolchain ok`;
   - нет → `ERROR toolchain not in PATH (…запуски кода кандидата падут на exit 127)`
     с подсказкой про PATH. Оператор видит причину в логе сразу при старте.

## Проверка
- go build/vet/test sandbox — ok.
- Live: healthz ok; POST /runs (go-fizzbuzz, верное решение) → passed=true.
- Стартовый лог содержит `toolchain ok` (go, python3) либо явный ERROR.
