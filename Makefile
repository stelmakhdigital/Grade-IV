# Сервис «Грейд» — основные команды (WP-1).
GO_MODULES := services/api services/sandbox

.PHONY: help install test build run-api run-sandbox run-voice run-frontend up down clean

help:
	@echo "Команды:"
	@echo "  make install     — зависимости (Go, pnpm, Python venv)"
	@echo "  make test        — все тесты (go vet+test, vitest, pytest)"
	@echo "  make build       — сборка (go build, vite build)"
	@echo "  make run-api     — запустить api (dev, :8000)"
	@echo "  make run-sandbox — запустить sandbox (dev, :8200)"
	@echo "  make run-voice   — запустить voice (dev, :8100)"
	@echo "  make run-frontend — запустить frontend (dev, :5173)"
	@echo "  make up          — docker compose (profile prod)"
	@echo "  make down        — остановить compose"
	@echo "  make clean       — убрать артефакты сборки"

install:
	@for m in $(GO_MODULES); do (cd $$m && go mod download) || exit 1; done
	@cd services/frontend && pnpm install
	@cd services/voice && python3 -m venv .venv && .venv/bin/pip install -q -r requirements.txt

test:
	@set -e; for m in $(GO_MODULES); do echo "=== go test $$m ==="; (cd $$m && go vet ./... && go test ./...); done
	@echo "=== vitest (frontend) ==="
	@cd services/frontend && pnpm -s test
	@echo "=== pytest (voice) ==="
	@cd services/voice && .venv/bin/python -m pytest -q

build:
	@(cd services/api && go build -o bin/api ./cmd/api)
	@(cd services/sandbox && go build -o bin/sandbox ./cmd/sandbox)
	@cd services/frontend && pnpm -s build

run-api:
	@(cd services/api && go run ./cmd/api)

run-sandbox:
	@(cd services/sandbox && go run ./cmd/sandbox)

run-voice:
	@(cd services/voice && .venv/bin/uvicorn app.main:app --port 8100)

run-frontend:
	@(cd services/frontend && pnpm dev)

up:
	@docker compose -f infra/docker-compose.yml --profile prod up --build

down:
	@docker compose -f infra/docker-compose.yml --profile prod down

clean:
	@rm -rf services/api/bin services/sandbox/bin services/frontend/dist
