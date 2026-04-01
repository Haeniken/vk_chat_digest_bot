FROM golang:1.24 AS builder
WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/bot ./cmd/bot

FROM gcr.io/distroless/static-debian12
WORKDIR /app
COPY --from=builder /out/bot /app/bot
COPY .env.example /app/.env.example
ENTRYPOINT ["/app/bot"]
