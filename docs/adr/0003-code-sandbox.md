# ADR-003. Сандбокс кода: Docker-контейнер на сессию

**Статус:** принято (2026-09-09)

## Контекст
Live-Code (FR-C1…C3, US-4): запуск кода и тестов кандидата (Go/Python) с гарантированной
изоляцией (NFR-4), результатом ≤ 10 с, без доступа в сеть и хост-ресурсам.

## Решение
**Живой (long-lived) Docker-контейнер на сессию** — создаётся на старте стадии Live-Code,
уничтожается по завершении сессии:
- образы: `golang:1.24` / `python:3.12-slim` (под готовыми каталогами задач);
- запуск: `--network=none --cpus=1 --memory=512m --pids-limit=128 --read-only
  --tmpfs /work:size=256m --user 1000:1000` (нет сети, лимиты CPU/RAM/процессов,
  rootfs read-only, непривилегированный пользователь);
- код передаётся в контейнер через API (tar в `/work`), команды по задаче:
  Go — `go test ./...`, Python — `pytest -q` (интерпретатор уже в образе);
  таймаут запуска 10 с, выход — `{exit_code, stdout, stderr, duration_ms, tests[]}`;
- **API**: `POST /api/v1/sessions/{id}/runs` {files, action:"test"} → результат.
- **Dev-fallback** (Docker недоступен): `SANDBOX_MODE=subprocess` — `subprocess` с
  RLIMIT_CPU/RLIMIT_AS/RLIMIT_NPROC, чистый env, изолированный cwd. Только для локальной
  разработки; в prod запуск в subprocess-режиме запрещён (fail-closed).

## Альтернативы
- **Контейнер на каждый run**: + максимальная изоляция, − 1–3 с на старт + стоимость
  образов при каждом запуске. Отклонено (лимиты + read-only + user 1000 достаточно).
- **WASM/Pyodide**: Python частично, Go — практически нет; тест-сьюты и native-расширения
  неполноценны. Отклонено.
- **Firecracker/microVM**: избыточно для MVP, операционная сложность.

## Последствия
- + Честные runtime Go/Python; детерминированный результат тестов.
- + Лимиты на уровне Docker — надёжнее, чем soft-limits процесса.
- − Нужен Docker-демон на узле (prod: docker-хост; локально: Docker Desktop).
- − `--network=none`: `pip install`/`go mod download` в тестах невозможны — зависимости
  фиксируются в банке задач (образа/каталоги задач предподготовлены).
- − Бюджет дисков: образы ≈ 1.5 ГБ (уложено в доступные 11 ГБ dev-машины; compose делает
  `pull` один раз).

## Поправка (2026-10-09, T-20261009133832): RLIMIT для subprocess-режима

Subprocess-режим (dev, docker недоступен) получил rlimit-аналог лимитов docker:
- **RLIMIT_AS**: go — 2048 МБ VA (холодная сборка std в per-run GOCACHE требует
  >1.5 ГБ VA, измерено; Go-рантайм/компилятор резервируют VA много больше RSS),
  python — 512 МБ (≈ docker --memory=512m; для python VA≈RSS);
- **RLIMIT_CPU**: 10 с (как docker-режим: результат ≤ 10 с);
- механизм — sh-proлог `ulimit -v <KB>; ulimit -t <с>; <команда>` (лимиты
  наследуются всем дочерним процессам, ставятся до команды, без parent-состояния
  и без гонки). **Отклонение от первоначального плана** («syscall.Setrlimit через
  SysProcAttr»): Go stdlib `syscall.SysProcAttr` **не имеет поля Rlimit** (только
  x/sys/unix, не совместимый с `os/exec.Cmd.SysProcAttr *syscall.SysProcAttr`) —
  ulimit-пролог даёт те же семантики без новой зависимости (sandbox-модуль
  остаётся без require-блока).
- Маркировка результатов: процесс, убитый исчерпанием RLIMIT (128+SIGXCPU,
  137/SIGKILL ядра при превышении CPU-лимита на >10%, SIGXCPU напрямую) →
  exit 124 + timeout=true (семантика «прерван по лимиту ресурсов»). Known
  limitation: 137 даёт и осознанный os.kill(self, SIGKILL) кандидата — принимается
  как «запуск прерван».
- **VA ≠ RSS**: rlimit ограничивает виртуальную память, а cgroup --memory — RSS;
  точный RSS-бюджет в subprocess rlimit не выразить (RLIMIT_RSS на Linux не
  поддерживается). Защита dev-режима — от «виснет/OOM-хост» (ранее 2 ГБ
  аллокация уходила в OOM-killer ядра; теперь MemoryError/быстрая смерть),
  не точный бюджет — точный бюджет даёт только docker-режим (prod).
- Тесты: TestSubprocessMemoryLimit (2 ГБ → MemoryError, 0.4 с),
  TestSubprocessCPULimit (вечный цикл, CPU 2 с → ~2 с, 124/timeout),
  TestBankSubprocessRegression (12 задач банка исполняются, поведение не изменилось).
- Docker-пробы (CI): джоб sandbox-docker + TestSandboxDockerProbe (opt-in
  SANDBOX_DOCKER=1) + scripts/ci-docker-test.sh — network=none (go-тест с внешним
  dial), py-pass, py-oom (2 ГБ → 137, без timeout).
