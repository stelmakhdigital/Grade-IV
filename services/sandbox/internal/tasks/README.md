# Банк задач Live-Code

Задачи встраиваются в бинарник sandbox (`go:embed tasks.json`) — сеть для загрузок не нужна.
Загрузка и дедупликация id: `internal/tasks/tasks.go`. API: `GET /api/v1/tasks?stack=&grade=`,
`POST /runs` (прокси api). Тесты банка: `go test ./...` в `services/sandbox`
(включая `TestBankSubprocessRegression` — регрессия: stub-решение каждой задачи должно
проваливаться быстрой ошибкой, а не таймаутом).

## Schema задачи

```json
{
  "id": "go-… | py-…",          // уникальный, в стиле стека
  "stack": "go | python",
  "grades": ["junior | middle | senior | staff"],
  "title": "короткое название",
  "statement": "условие (RU): API по сигнатурам starter'а, правила, крайние случаи",
  "files": {
    "go.mod": "module task\n\ngo 1.21\n",      // только для go
    "solution.go": "starter с TODO (или solution.py)",
    "solution_test.go": "тесты (или solution_test.py)"
  }
}
```

Требования к задаче:

- **Стек go**: `go.mod` + `solution.go` (starter: сигнатуры + `// TODO`) + `solution_test.go`
  (package task, только stdlib, `testing`).
- **Стек python**: `solution.py` (starter) + `solution_test.py` (unittest — работает и под
  pytest, и под `python3 -m unittest`; `from solution import …`).
- **Детерминизм**: без реального времени/сети/рандомности результата; время — только
  inject-функцией/параметром (`clock`, `sleep`, `now`); фиксированные seed для rand.
- **TTL решения**: ~15–20 минут.
- **Starter не должен виснуть**: stub-решение обязано проваливаться тестами за секунды
  (`TestBankSubprocessRegression` гоняет stub'ы; блокирующие конструкции в тестах — через
  `select`/default или non-blocking-аналоги).
- **Тесты** должны падать на stub'е и проходить на референсе; поведение, зафиксированное
  тестами, должно точно совпадать с формулировкой в `statement`.

## Как добавить задачу

1. **Создать задачу в `tasks.json`** — точечная правка JSON (без Python-генераторов с
   экранированными строками): id/stack/grades/title/statement/files (см. schema).
   Проверить: `python3 -m json.tool tasks.json > /dev/null` + `go test ./...` в sandbox.
2. **Референс-решение** в `reference/<id>.go|.py` — полный вариант `solution.go|.py`
   (референс НЕ попадает в `files`!). Файлы `reference/*.go` несут тег `//go:build ignore`,
   чтобы каталог не ломал сборку пакета; при прогоне тег срезается.
3. **Прогнать референс** (локальный запуск тестов в каталоге сборки):
   - выписать файлы задачи в каталог (например, `go test` / `python3 -m unittest`
     в `/tmp/<id>`), подменить `solution.*` референсом;
   - все тесты — зелёные; результат — в таблицу ниже.
   - (опционально) через поднятый sandbox: `POST /runs` с `files` задачи, где
     `solution.*` = референс.
4. **Коммит порционный** (3–5 задач), каждый — валидный JSON + зелёные `go test ./...`
   в sandbox.

## Таблица задач

| id | stack | grades | прогон |
|---|---|---|---|
| go-reverse-string | go | junior | существующая (банк) |
| go-two-sum | go | junior | существующая (банк) |
| go-fizzbuzz | go | junior, middle | существующая (банк) |
| go-strstr | go | middle | существующая (банк) |
| go-lru-cache | go | senior, staff | существующая (банк) |
| go-parallel-sum | go | senior, staff | существующая (банк) |
| py-palindrome | python | junior | существующая (банк) |
| py-two-sum | python | junior | существующая (банк) |
| py-word-count | python | junior, middle | существующая (банк) |
| py-flatten | python | middle | существующая (банк) |
| py-lru-cache | python | senior | существующая (банк) |
| py-rate-limiter | python | senior, staff | существующая (банк) |
| go-longest-common-prefix | go | junior | существующая (банк) |
| go-most-frequent-word | go | junior | существующая (банк) |
| go-parse-kv | go | junior | существующая (банк) |
| go-merge-k-sorted | go | middle | существующая (банк) |
| go-top-k-freq | go | middle | существующая (банк) |
| go-token-bucket | go | middle | существующая (банк) |
| go-worker-pool | go | senior | существующая (банк) |
| go-ttl-cache | go | senior | существующая (банк) |
| go-circuit-breaker | go | senior | существующая (банк) |
| go-consistent-hash | go | staff | существующая (банк) |
| go-shard-scheduler | go | staff | существующая (банк) |
| py-rle | python | junior | существующая (банк) |
| py-parse-log-line | python | junior | существующая (банк) |
| py-group-anagrams | python | junior | существующая (банк) |
| py-merge-intervals | python | middle | существующая (банк) |
| py-top-k-smallest | python | middle | существующая (банк) |
| py-sliding-window-limit | python | middle | существующая (банк) |
| py-ttl-cache | python | senior | существующая (банк) |
| py-circuit-breaker | python | senior | существующая (банк) |
| py-topological-order | python | senior | существующая (банк) |
| py-consistent-hash | python | staff | существующая (банк) |
| py-lease-lock | python | staff | существующая (банк) |
| go-reverse-linked-list | go | junior | зелёный (12.10, референс) |
| go-ring-buffer-queue | go | junior | зелёный (12.10, референс) |
| go-roman-to-int | go | junior | зелёный (12.10, референс) |
| go-config-kv-parser | go | junior | зелёный (12.10, референс) |
| go-generic-stack-queue | go | middle | зелёный (12.10, референс) |
| go-min-heap | go | middle | зелёный (12.10, референс) |
| go-middleware-chain | go | middle | зелёный (12.10, референс) |
| go-pubsub | go | middle | зелёный (12.10, референс) |
| py-valid-parentheses | python | junior | зелёный (12.10, референс) |
| py-url-query-parser | python | junior | зелёный (12.10, референс) |
| py-merge-two-sorted | python | junior | зелёный (12.10, референс) |
| py-retry-decorator | python | middle | зелёный (12.10, референс) |
| py-event-bus | python | middle | зелёный (12.10, референс) |
| py-generator-pipeline | python | middle | зелёный (12.10, референс) |
| py-protocol-parser | python | junior | зелёный (12.10, референс) |

Всего: 49 задач (go 25 / python 24). Новые 15 (≥7 go + ≥7 py) — прогнаны референсами
12.10, все тесты зелёные.
