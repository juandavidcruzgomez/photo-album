# syntax=docker/dockerfile:1
#
# Images for the api (with the embedded web app), the indexer and the
# crawler. Build one with --target api|indexer|crawler, or through
# docker/docker-compose.local.yml.
#
# The ONNX Runtime library and the CLIP models are downloaded for the target
# architecture (x86_64 or arm64), so the api and indexer images are
# self-contained. To build for another architecture, use buildx --platform: the
# web app and Go binaries build natively, and the rest needs QEMU emulation
# (see rpi-setup in the Makefile).

ARG GO_VERSION=1.25
ARG NODE_VERSION=20.19
ARG DEBIAN=bookworm

# --- Web app ------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM node:${NODE_VERSION}-${DEBIAN}-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# --- ONNX Runtime + CLIP models ---------------------------------------------------
FROM debian:${DEBIAN}-slim AS models
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY scripts/fetch-models.sh scripts/
RUN scripts/fetch-models.sh

# --- Go binaries ----------------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-${DEBIAN} AS build
ARG TARGETARCH
# cgo needs a C cross-compiler when building arm64 on another architecture.
RUN if [ "$TARGETARCH" = arm64 ] && [ "$(dpkg --print-architecture)" != arm64 ]; then \
      apt-get update \
      && apt-get install -y --no-install-recommends gcc-aarch64-linux-gnu libc6-dev-arm64-cross \
      && rm -rf /var/lib/apt/lists/*; \
    fi
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
COPY --from=web /src/web/dist web/dist
ENV GOFLAGS=-trimpath GOOS=linux GOARCH=$TARGETARCH
# api and indexer load ONNX Runtime through cgo; the crawler is pure Go.
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    export CC="$([ "$TARGETARCH" = arm64 ] && echo aarch64-linux-gnu-gcc || echo gcc)" \
 && CGO_ENABLED=1 go build -ldflags="-s -w" -o /out/api ./cmd/api \
 && CGO_ENABLED=1 go build -ldflags="-s -w" -o /out/indexer ./cmd/indexer \
 && CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/crawler ./cmd/crawler

# --- Runtime --------------------------------------------------------------------
FROM debian:${DEBIAN}-slim AS base
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates tzdata \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --uid 1000 --user-group --no-create-home --shell /usr/sbin/nologin phalbum
USER phalbum

FROM base AS clip
ENV ONNX_LIB=/opt/phalbum/models/libonnxruntime.so.1.31.0 \
    CLIP_MODELS_DIR=/opt/phalbum/models/clip-vit-base-patch32 \
    CLIP_TEXT_MODELS_DIR=/opt/phalbum/models/clip-ViT-B-32-multilingual-v1
COPY --from=models /src/models /opt/phalbum/models
# Fail the build, not the container, if a model file is missing.
RUN for f in "$ONNX_LIB" "$CLIP_MODELS_DIR"/vision_model_quantized.onnx "$CLIP_MODELS_DIR"/text_model_quantized.onnx \
      "$CLIP_MODELS_DIR"/vocab.json "$CLIP_MODELS_DIR"/merges.txt \
      "$CLIP_TEXT_MODELS_DIR"/text_model_quantized.onnx "$CLIP_TEXT_MODELS_DIR"/vocab.txt \
      "$CLIP_TEXT_MODELS_DIR"/dense.safetensors; do \
      test -s "$f" || { echo "missing $f" >&2; exit 1; }; \
    done

FROM clip AS api
ENV API_ADDR=:8090
COPY --from=build /out/api /usr/local/bin/api
EXPOSE 8090
HEALTHCHECK --interval=30s --timeout=3s --start-period=20s \
  CMD ["/usr/local/bin/api", "-healthcheck"]
ENTRYPOINT ["/usr/local/bin/api"]

FROM clip AS indexer
COPY --from=build /out/indexer /usr/local/bin/indexer
ENTRYPOINT ["/usr/local/bin/indexer"]

FROM base AS crawler
COPY --from=build /out/crawler /usr/local/bin/crawler
ENTRYPOINT ["/usr/local/bin/crawler"]
