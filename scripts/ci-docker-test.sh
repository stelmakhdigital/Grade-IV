#!/usr/bin/env bash
# Sandbox docker-пробы — локальный прогон (зеркало CI-джоба sandbox-docker).
# 3 задачи в docker-режиме (ADR-003: network=none, --memory=512m, --cpus=1,
# --pids-limit=128): go-pass (+ proof network=none), py-pass, py-oom (OOM-kill
# 137). Проверяет Result-контракт (passed/timeout/exit_code).
#
# Использование: bash scripts/ci-docker-test.sh
# Требования: docker CLI + запущенный демон (в среде без docker — валидация
# структуры только в CI/локально с docker; exit 2 — docker недоступен).
set -euo pipefail
cd "$(dirname "$0")/.."

if ! command -v docker >/dev/null 2>&1; then
  echo "docker CLI недоступен — docker-пробы невозможно выполнить (валидация структуры: yamllint/CI)" >&2
  exit 2
fi
if ! docker info >/dev/null 2>&1; then
  echo "docker-демон не запущен (или нет прав) — docker-пробы невозможно выполнить" >&2
  exit 2
fi

cd services/sandbox
docker image inspect golang:1.24 >/dev/null 2>&1 || docker pull golang:1.24
docker image inspect python:3.12-slim >/dev/null 2>&1 || docker pull python:3.12-slim

SANDBOX_DOCKER=1 go test -count=1 -v -run TestSandboxDockerProbe ./internal/runner/
