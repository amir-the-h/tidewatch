# ──────────────────────────────────────────────
# Build stage
# ──────────────────────────────────────────────
FROM golang:1.25-alpine AS builder

# NOTE: When daulet/tokenizers and yalue/onnxruntime_go are wired in
# (the ONNX pipeline TODOs), this stage will need:
#   - Rust toolchain: apk add --no-cache rust cargo
#   - ONNX Runtime dev headers and shared lib
#   - CGO_ENABLED=1
# For now, the binary is pure Go so we keep it simple.

RUN apk add --no-cache git

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o iran-watcher ./cmd/iran-watcher

# ──────────────────────────────────────────────
# Runtime stage
# ──────────────────────────────────────────────
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /app/iran-watcher /usr/local/bin/iran-watcher

# Copy default config (can be overridden by mounting a config file)
RUN mkdir -p /configs /data /models
COPY configs/iran-watcher.yaml /configs/iran-watcher.yaml

VOLUME ["/data", "/models"]

EXPOSE 8080

# Sensible container defaults — all can be overridden via environment variables
# or by mounting a config file at /configs/iran-watcher.yaml.
ENV IRAN_WATCHER_SQLITE_PATH=/data/iran-watcher.db \
    IRAN_WATCHER_MODELS_DIR=/models \
    IRAN_WATCHER_API_BIND=0.0.0.0:8080

ENTRYPOINT ["iran-watcher"]
CMD ["serve", "--config", "/configs/iran-watcher.yaml"]
