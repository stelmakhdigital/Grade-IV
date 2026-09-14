

# Сервис «Грейд» — основные команды (WP-1; WP-12: up-gpu, smoke).
GO_MODULES := services/api services/sandbox

.PHONY: help install test build run-api run-sandbox run-voice run-frontend up up-gpu down smoke clean

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
	@echo "  make up-gpu      — compose prod + gpu (voice на GPU-узле)"
	@echo "  make down        — остановить compose"
	@echo "  make smoke       — REST e2e-смоук на живом api (API=… , def :8877)"
	@echo "  make clean       — убрать артефакты сборки"

install:
	@for m in $(GO_MODULES); do (cd $$m && go mod download) || exit 1; done
	@cd services/frontend && pnpm install
	# voice: venv без ensurepip (get-pip bootstrap) + torch CPU ПЕРЕД requirements
	# (иначе pip потянет nvidia-* ~2 ГБ; на GPU-узле torch ставится с CUDA-индексом).
	@cd services/voice && rm -rf .venv && python3 -m venv --without-pip .venv \
		&& curl -fsSL -o .get-pip.py https://bootstrap.pypa.io/get-pip.py \
		&& .venv/bin/python .get-pip.py -q && rm .get-pip.py \
		&& .venv/bin/pip install -q torch --index-url https://download.pytorch.org/whl/cpu \
		&& .venv/bin/pip install -q -r requirements.txt

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

up-gpu:
	@docker compose -f infra/docker-compose.yml --profile prod --profile gpu up --build

smoke:
	@bash scripts/smoke.sh

clean:
	@rm -rf services/api/bin services/sandbox/bin services/frontend/dist
