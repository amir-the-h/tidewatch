# ──────────────────────────────────────────────
# Build stage
# ──────────────────────────────────────────────
# Debian-based image required for CGO + Rust + CUDA-capable ONNX Runtime.
FROM golang:1.25-bookworm AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
    git gcc g++ make wget tar ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Download pre-built libtokenizers.a for linux-amd64.
# Source: https://github.com/daulet/tokenizers/releases
ARG TOKENIZERS_VERSION=1.26.0
RUN wget -q "https://github.com/daulet/tokenizers/releases/download/v${TOKENIZERS_VERSION}/libtokenizers.linux-amd64.tar.gz" \
        -O /tmp/libtokenizers.tar.gz \
    && tar -xzf /tmp/libtokenizers.tar.gz -C /usr/local/lib \
    && rm /tmp/libtokenizers.tar.gz

# Download ONNX Runtime 1.24.1 GPU (CUDA 12) for linux-x64.
# Source: https://github.com/microsoft/onnxruntime/releases
ARG ORT_VERSION=1.24.1
RUN wget -q "https://github.com/microsoft/onnxruntime/releases/download/v${ORT_VERSION}/onnxruntime-linux-x64-gpu-${ORT_VERSION}.tgz" \
        -O /tmp/ort.tgz \
    && mkdir -p /tmp/ort \
    && tar -xzf /tmp/ort.tgz -C /tmp/ort --strip-components=1 \
    && cp /tmp/ort/lib/libonnxruntime.so.${ORT_VERSION}          /usr/local/lib/ \
    && cp /tmp/ort/lib/libonnxruntime_providers_shared.so         /usr/local/lib/ \
    && cp /tmp/ort/lib/libonnxruntime_providers_cuda.so           /usr/local/lib/ \
    && rm -rf /tmp/ort /tmp/ort.tgz

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 GOOS=linux \
    CGO_LDFLAGS="-L/usr/local/lib" \
    go build -ldflags="-s -w" -o iran-watcher ./cmd/iran-watcher

# ──────────────────────────────────────────────
# Runtime stage
# ──────────────────────────────────────────────
# CUDA 12 runtime image provides libcudart, libcublas, etc. needed by ORT CUDA EP.
# Requires --gpus all (or equivalent) when running the container.
FROM nvidia/cuda:12.6.0-cudnn-runtime-ubuntu22.04

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/*

# Copy ONNX Runtime shared libraries.
ARG ORT_VERSION=1.24.1
COPY --from=builder /usr/local/lib/libonnxruntime.so.${ORT_VERSION}    /usr/local/lib/
COPY --from=builder /usr/local/lib/libonnxruntime_providers_shared.so   /usr/local/lib/
COPY --from=builder /usr/local/lib/libonnxruntime_providers_cuda.so     /usr/local/lib/
RUN ldconfig

COPY --from=builder /app/iran-watcher /usr/local/bin/iran-watcher

RUN mkdir -p /configs /data /models
COPY configs/iran-watcher.yaml /configs/iran-watcher.yaml

VOLUME ["/data", "/models"]

EXPOSE 8080

ENV IRAN_WATCHER_SQLITE_PATH=/data/iran-watcher.db \
    IRAN_WATCHER_MODELS_DIR=/models \
    IRAN_WATCHER_API_BIND=0.0.0.0:8080 \
    IRAN_WATCHER_ONNX_LIB_PATH=/usr/local/lib/libonnxruntime.so.${ORT_VERSION}

ENTRYPOINT ["iran-watcher"]
CMD ["serve", "--config", "/configs/iran-watcher.yaml"]
