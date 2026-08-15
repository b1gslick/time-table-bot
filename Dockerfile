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

FROM debian:bookworm-slim AS whisper-builder

ARG WHISPER_CPP_VERSION=v1.9.2
ARG WHISPER_CPP_SHA256=a6abd064fcca8b85e794d205abf328c522e9451db43a3eadc178b883b7d0e9cd
ARG WHISPER_MODEL=base-q5_1
ARG WHISPER_MODEL_SHA256=422f1ae452ade6f30a004d7e5c6a43195e4433bc370bf23fac9cc591f01a8898

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates cmake curl g++ make \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /build
RUN curl -fsSL --retry 5 -o whisper.tar.gz "https://github.com/ggml-org/whisper.cpp/archive/refs/tags/${WHISPER_CPP_VERSION}.tar.gz" \
    && echo "${WHISPER_CPP_SHA256}  whisper.tar.gz" | sha256sum -c - \
    && mkdir source \
    && tar -xzf whisper.tar.gz -C source --strip-components=1 \
    && cmake -S source -B source/build \
        -DCMAKE_BUILD_TYPE=Release \
        -DBUILD_SHARED_LIBS=OFF \
        -DGGML_NATIVE=OFF \
        -DGGML_OPENMP=OFF \
        -DWHISPER_BUILD_TESTS=OFF \
        -DWHISPER_BUILD_SERVER=OFF \
    && cmake --build source/build --config Release --target whisper-cli -j2 \
    && curl -fsSL --retry 5 -o "ggml-${WHISPER_MODEL}.bin" "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-${WHISPER_MODEL}.bin" \
    && echo "${WHISPER_MODEL_SHA256}  ggml-${WHISPER_MODEL}.bin" | sha256sum -c -

FROM debian:bookworm-slim

ARG WHISPER_MODEL=base-q5_1

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates ffmpeg \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /out/time-table-bot /app/time-table-bot
COPY --from=whisper-builder /build/source/build/bin/whisper-cli /usr/local/bin/whisper-cli
COPY --from=whisper-builder /build/ggml-${WHISPER_MODEL}.bin /models/ggml-${WHISPER_MODEL}.bin

ENV WHISPER_CLI_PATH=/usr/local/bin/whisper-cli \
    WHISPER_FFMPEG_PATH=/usr/bin/ffmpeg \
    WHISPER_MODEL_PATH=/models/ggml-${WHISPER_MODEL}.bin \
    WHISPER_THREADS=2

USER 65532:65532
ENTRYPOINT ["/app/time-table-bot"]
