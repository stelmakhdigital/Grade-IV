# Разработка

Тулчейн, команды, как проверять себя. Прод — [OPERATIONS.md](OPERATIONS.md).

## Версии

| | |
|---|---|
| Go | 1.26 (CI: `go-version: "1.26"`) |
| Node.js | 22 (`engines.node`, `.nvmrc`) |
| pnpm | 10 (`packageManager: pnpm@10.34.9`) — на pnpm 12 сборка падает с `ERR_PNPM_IGNORED_BUILDS` |
| Python | 3.12 (CI и образ voice) |

## Тема интерфейса

Две темы: тёмная (базовая, значения в `:root` файла `src/tokens.css`) и светлая
(`[data-theme='light']` там же). Выбор — `src/theme.ts`: по умолчанию системная
(`prefers-color-scheme`), переключатель в шапке кабины и админки
(`src/ThemeToggle.tsx`) закрепляет его в `localStorage`, «Авто» возвращает
следование системе. Атрибут ставится на `<html>` до первого кадра (`main.tsx`).

Правило для новых стилей: только токены. Белый с альфой и затемнение оверлея —
`--veil-1..3` и `--scrim` (в светлой теме они чернильные: `rgba(255,255,255,.1)`
на бумаге невиден). Хардкод `#fff`/`#000`/`rgba(255,…)` в `index.css` — ошибка
ревью: свечение темы проверяется глазами в обеих (скриншоты кабине, админки и
отчёта), контраст текста — не ниже AA. Лендинг (`styles.css` в корне) остаётся
тёмным: он собирается без vite и своей палитрой живёт отдельно.


## Дев-контур

```bash
make install                    # go mod download + pnpm install + venv voice (torch CPU)
bash scripts/download-models.sh # ~600 МБ: GigaAM v3 + Silero TTS
make run-all                    # sandbox + voice + api + frontend на :5173
make status ; make stop-all
```

Без LLM-узла достаточно `LLM_MOCK=1` — интервьюер отвечает детерминированными
репликами (ADR-005), все сценарии проходят. Отдельные сервисы: `make run-api`,
`run-sandbox`, `run-voice`, `run-frontend`.

## Проверка себя

```bash
make test      # go vet+test обеих моделей, токены, vitest, pytest
make build     # бинарники api/sandbox, dist фронтенда
make smoke     # bash scripts/smoke.sh против поднятого стека
```

Частичные прогоны:

```bash
cd services/api && go vet ./... && go test ./...
cd services/frontend && pnpm exec tsc --noEmit && pnpm exec vitest run
cd services/voice && .venv/bin/python -m pytest -q
```

### Go на машине без Go

В CI Go стоит на раннере, локально его может не быть — тогда всё то же в контейнере.
Кэш модулей и сборки держим в git-ignored каталогах репозитория, повторные прогоны
быстрые и сети не требуют:

```bash
docker run --rm -u $(id -u):$(id -g) \
  -v $PWD/.gomod:/gomod -v $PWD/.gocache:/gocache \
  -e GOMODCACHE=/gomod -e GOCACHE=/gocache -e GOPROXY=off -e HOME=/tmp \
  -v $PWD/services/api:/src -w /src golang:1.26 \
  sh -c 'gofmt -l . && go vet ./... && go test ./...'
```

Для sandbox берём debian-образ (`golang:1.26`, не `-alpine`): тесты shell-задач
зовут `bash`, в alpine его нет, и 12 тестов падают с `exit 127` на пустом месте.
Первый прогон без кэша требует сети (`GOPROXY=off` убирается, `go mod download`).

### Тесты фронтенда

`vitest run` — обычный порядок; CI гоняет `--sequence.shuffle --sequence.seed 1`,
потому что тесты одного файла делят один jsdom и видят состояние соседа (адрес в
hash, черновики кода, `vi.stubGlobal`). Прогон с другим порядком локально:
`pnpm exec vitest run --sequence.shuffle --sequence.seed <n>`.

Тесты не должны зависеть от порядка: адрес сбрасывается в `src/test/setup.ts`,
модульные синглтоны (например `codeCache`) очищаются в `beforeEach`, подменённые
глобалы откатываются `vi.unstubAllGlobals()`.

## Конвенции

- Дефолты живут в одном месте на сервис: api — `internal/config/config.go`,
  voice — `services/voice/app/main.py`, прод — `infra/docker-compose.yml`, шаблон —
  корневой `.env.example`. Расходятся — править в сторону кода, а не наоборот.
  Отдельное правило про STT — `AGENTS.md`.
- Ссылки вида `ADR-00N`, `spec-NN`, `ARCHITECTURE.md §N` — на [DECISIONS.md](DECISIONS.md),
  [ITERATIONS.md](history/ITERATIONS.md) и разделы архитектуры; новые решения —
  новым номером, старый номер не переиспользовать.
- `gofmt` — часть `make test` и CI (`gofmt -l` блокирует).
- Добавление задачи в банк Live-Code — `services/sandbox/internal/tasks/README.md`.

## Что где лежит

`services/{frontend,api,voice,sandbox}` — сервисы; `infra/` — compose, caddy,
docker-образы, мониторинг; `scripts/` — `run-all.sh`, `download-models.sh`,
`smoke.sh`, `check-tokens.mjs`; `docs/history/` — история итераций и разборы инцидентов;
корневые `index.html`/`script.js`/`styles.css` — лендинг.
