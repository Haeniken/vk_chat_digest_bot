FROM golang:1.26.2 AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/bot ./cmd/bot

FROM alpine:3.22.2
RUN apk add --no-cache ca-certificates
COPY certs/*.crt /usr/local/share/ca-certificates/
RUN update-ca-certificates
WORKDIR /app
COPY --from=builder /out/bot /app/bot
COPY .env.example /app/.env.example
ENTRYPOINT ["/app/bot"]
