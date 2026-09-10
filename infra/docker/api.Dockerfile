# grade-api (Go; чистая сборка, CGO_ENABLED=0 — modernc.org/sqlite)
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/api /usr/local/bin/api
EXPOSE 8000
ENTRYPOINT ["api"]
