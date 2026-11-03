# BUG-20261103-025 — docker-режим python: в python:3.12-slim нет pytest → все python-задачи падают

**Обнаружен:** 2026-11-03, CI-джоба sandbox-docker, проба py-pass (лог — ветка `ci-debug`).
**Статус:** исправлен (infra/docker/sandbox-python.Dockerfile + env-оверрайды образов).
**Серьёзность:** блокер python-задач в docker-режиме (prod compose).

## Симптом

Проба py-pass (и любой python-запуск в docker-режиме):

```
/usr/local/bin/python3: No module named pytest
```

## Причина

Раннер исполняет задачи банка командой `python3 -m pytest -q` (subprocess —
pytest установлен на хосте; docker — образ `python:3.12-slim` pytest не
содержит). Установить на прогон нельзя: контейнер с `--network=none`,
pip требует сеть.

## Фикс

1. `infra/docker/sandbox-python.Dockerfile` — образ для задач:
   `FROM python:3.12-slim` + `pip install --no-cache-dir pytest==8.3.4`
   (версия запинена).
2. Оверрайды образов через env: `SANDBOX_IMAGE_GO` / `SANDBOX_IMAGE_PYTHON`
   / `SANDBOX_IMAGE_SHELL` (runner.Config.Images мерджится на дефолты —
   частичный оверрайд не сбрасывает остальные стеки).
3. compose: сервис `python-tests` собирает образ при `up --build`, sandbox
   подхватывает через `SANDBOX_IMAGE_PYTHON=grade/python-tests:dev`.
4. CI-джоба sandbox-docker строит образ и подставляет его в пробы
   (`envImages()` в docker-тестах раннера).

## Регресс

`SANDBOX_DOCKER=1 SANDBOX_IMAGE_PYTHON=grade/python-tests:dev go test -run
Docker ./internal/runner/` — py-pass и py-oom на образе с pytest.
