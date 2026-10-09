# grade-sandbox (Go)
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
