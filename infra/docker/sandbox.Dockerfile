# grade-sandbox (Go)
#
# Как код кандидатов попадает в контейнер. Прод-режим (SANDBOX_MODE=docker)
# поднимает контейнер на каждый прогон через `docker run`, поэтому процессу
# нужен docker-демон. Сырой /var/run/docker.sock сюда не монтируется: вместо
# него tcp-прокси со списком разрешённых маршрутов (сервис docker-proxy в
# compose), и сам контейнер работает под непривилегированным пользователем.
#
# Что этим закрывается: exec в чужие контейнеры, networks / volumes / plugins /
# secrets / configs / swarm / nodes / tasks, сборка образов (build), коммит
# контейнеров (commit), /auth (учётки registry), общий поток событий.
# Что остаётся: POST /containers/create для прогонов неизбежен, а с ним можно
# попросить демон примонтировать корень хоста. Путь до root на хосте не закрыт
# для того, кто сначала получил RCE в этом сервисе. Снятие этой угрозы —
# rootless-демон или исполнитель на отдельном uid, задача не на этот diff.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download || true
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/sandbox ./cmd/sandbox

FROM alpine:3.21
# docker-cli: SANDBOX_MODE=docker (prod, compose) — runner исполняет `docker run`,
# обращаясь к демону через DOCKER_HOST (tcp://docker-proxy:2375, см. compose);
# без CLI все запуски падали с 503 «docker недоступен на узле» (рассинхрон,
# T-20261009133832).
#
# /cache и /var/lib/grade-sandbox принадлежат пользователю песочницы не для
# косметики: docker собирает именованный том из содержимого и с владельцем
# каталога в образе в момент первого монтирования. Оставь здесь root — том
# достанется root'у, и песочница под --user не сможет писать ни в GOCACHE
# (на живом стенде: permission denied на каждом go-прогоне), ни в банк задач.
RUN apk add --no-cache ca-certificates tzdata docker-cli \
 && adduser -D -u 1000 -h /home/sandbox sandbox \
 && mkdir -p /cache/go-build /home/sandbox /var/lib/grade-sandbox \
 && chown -R sandbox:sandbox /cache /home/sandbox /var/lib/grade-sandbox
COPY --from=build /out/sandbox /usr/local/bin/sandbox
USER sandbox
ENV HOME=/home/sandbox
EXPOSE 8200
# 127.0.0.1, а не localhost: сервер слушает IPv4, а localhost в контейнере
# первым делом резолвится в ::1 (та же причина, что в frontend.Dockerfile).
HEALTHCHECK --interval=30s --timeout=5s --retries=3 CMD wget -qO- http://127.0.0.1:8200/healthz || exit 1
ENTRYPOINT ["sandbox"]
