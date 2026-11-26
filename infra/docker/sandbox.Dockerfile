# grade-sandbox (Go)
#
# ВНИМАНИЕ. В прод-профиле контейнер монтирует /var/run/docker.sock и запускает
# код кандидатов через `docker run` (SANDBOX_MODE=docker). Доступ к сокету =
# root на хосте: любой, кто достучался до /run, может поднять привилегированный
# контейнер от root. Поэтому USER здесь не ставить: непривилегированный процесс
# не сможет открыть сокет, и все прогоны упадут с 503 «docker недоступен», а
# привилегия никуда не денется — это security by obscurity хуже текущей картины.
#
# Что делает root внутри контейнера осмысленным:
#   1) SANDBOX_MODE=subprocess (rlimit-исполнитель внутри контейнера) +
#      USER sandbox + сокет наружу; или
#   2) docker-socket-proxy с allowlist (только create/start/logs/kill) вместо
#      сырого сокета.
# Оба варианта — отдельная задача, не косметика.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download || true
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/sandbox ./cmd/sandbox

FROM alpine:3.21
# docker-cli: SANDBOX_MODE=docker (prod, compose) — runner исполняет `docker run`
# через смонтированный сокет хоста (/var/run/docker.sock); без CLI все запуски
# падали с 503 «docker недоступен на узле» (рассинхрон, T-20261009133832).
RUN apk add --no-cache ca-certificates tzdata docker-cli
COPY --from=build /out/sandbox /usr/local/bin/sandbox
EXPOSE 8200
HEALTHCHECK --interval=30s --timeout=5s --retries=3 CMD wget -qO- http://localhost:8200/healthz || exit 1
ENTRYPOINT ["sandbox"]
