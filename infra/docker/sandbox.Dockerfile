# grade-sandbox (Go)
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download || true
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/sandbox ./cmd/sandbox

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/sandbox /usr/local/bin/sandbox
EXPOSE 8200
ENTRYPOINT ["sandbox"]
