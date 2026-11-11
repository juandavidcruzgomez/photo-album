# syntax=docker/dockerfile:1
#
# One image holding the api (with the embedded web app), the indexer, the
# crawler and scrub. It has no entrypoint: each compose service chooses its binary. The
# models aren't in the image: fetch-models downloads them into a volume at startup.
#
# To build for another architecture, use --platform. The web app and Go binaries
# build natively, and the rest needs QEMU emulation. See the README.

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
# api and indexer load ONNX Runtime through cgo; the crawler and scrub are pure Go.
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    export CC="$([ "$TARGETARCH" = arm64 ] && echo aarch64-linux-gnu-gcc || echo gcc)" \
 && CGO_ENABLED=1 go build -ldflags="-s -w" -o /out/api ./cmd/api \
 && CGO_ENABLED=1 go build -ldflags="-s -w" -o /out/indexer ./cmd/indexer \
 && CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/crawler ./cmd/crawler \
 && CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/scrub ./cmd/scrub

# --- Runtime --------------------------------------------------------------------
FROM debian:${DEBIAN}-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl tzdata \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --uid 1000 --user-group --no-create-home --shell /usr/sbin/nologin phalbum
USER phalbum
ENV API_ADDR=:8090 \
    MODELS_DIR=/opt/phalbum/models \
    ONNX_LIB=/opt/phalbum/models/libonnxruntime.so.1.31.0 \
    CLIP_MODELS_DIR=/opt/phalbum/models/clip-vit-base-patch32 \
    CLIP_TEXT_MODELS_DIR=/opt/phalbum/models/clip-ViT-B-32-multilingual-v1
COPY scripts/fetch-models.sh /usr/local/bin/fetch-models
COPY --from=build /out/ /usr/local/bin/
EXPOSE 8090
