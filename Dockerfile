FROM --platform=$BUILDPLATFORM golang:1.24-bookworm AS builder

ARG TARGETOS=linux
ARG TARGETARCH
ARG TARGETARM

WORKDIR /src

COPY go.mod go.sum* ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-$(go env GOARCH)} GOARM=${TARGETARM} go build -o /out/time-table-bot ./cmd/time-table-bot

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app
COPY --from=builder /out/time-table-bot /app/time-table-bot

USER nonroot:nonroot
ENTRYPOINT ["/app/time-table-bot"]
