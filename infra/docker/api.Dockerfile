# grade-api (Go; чистая сборка, CGO_ENABLED=0 — modernc.org/sqlite)
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 1000 grade
# Пишем: резюме кандидатов (/data/resumes — named volume) и правки админа в
# /app/config (bind-каталог репо). Права named volume наследуются от каталога в
# образе, поэтому создаём и отдаём их до USER.
RUN mkdir -p /data/resumes /app/config && chown -R grade:grade /data /app
# Под root api писал config/*.json в репозиторий root-овыми файлами, и
# перезаписать их без sudo было нельзя. В compose uid передаётся переменной
# GRADE_UID (bind-монтируемый config/ принадлежит хостовому пользователю).
USER grade
COPY --from=build /out/api /usr/local/bin/api
EXPOSE 8000
HEALTHCHECK --interval=30s --timeout=5s --retries=3 CMD wget -qO- http://localhost:8000/healthz || exit 1
ENTRYPOINT ["api"]
