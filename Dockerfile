FROM golang:1.22-alpine AS builder

WORKDIR /src

COPY go.mod go.sum* ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/time-table-bot ./cmd/time-table-bot

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app
COPY --from=builder /out/time-table-bot /app/time-table-bot

USER nonroot:nonroot
ENTRYPOINT ["/app/time-table-bot"]
