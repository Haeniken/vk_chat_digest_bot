FROM golang:1.26.5 AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/bot ./cmd/bot

FROM alpine:3.24.1
RUN apk add --no-cache ca-certificates \
    && addgroup -S bot \
    && adduser -S -D -H -G bot bot
COPY certs/*.crt /usr/local/share/ca-certificates/
RUN update-ca-certificates
WORKDIR /app
COPY --from=builder --chown=bot:bot /out/bot /app/bot
COPY --chown=bot:bot .env.example /app/.env.example
USER bot
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD [ "sh", "-c", "[ \"$(readlink /proc/1/exe)\" = /app/bot ]" ]
ENTRYPOINT ["/app/bot"]
