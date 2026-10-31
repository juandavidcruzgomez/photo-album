#!/usr/bin/env bash
# Downloads the ONNX Runtime shared library and the CLIP ONNX models used by
# internal/embedding into phalbum/models (git-ignored).
set -euo pipefail

ORT_VERSION="1.31.0" # must match the C API version used by onnxruntime_go
HF_REPO="Xenova/clip-vit-base-patch32"
HF_BASE="https://huggingface.co/${HF_REPO}/resolve/main"

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEST="${ROOT}/models"
mkdir -p "${DEST}/clip-vit-base-patch32"

case "$(uname -m)" in
  x86_64) ORT_ARCH="x64" ;;
  aarch64 | arm64) ORT_ARCH="aarch64" ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

ORT_LIB="${DEST}/libonnxruntime.so.${ORT_VERSION}"
if [[ ! -f "${ORT_LIB}" ]]; then
  echo "Downloading ONNX Runtime ${ORT_VERSION} (${ORT_ARCH})"
  tmp="$(mktemp -d)"
  trap 'rm -rf "${tmp}"' EXIT
  curl -fL --progress-bar -o "${tmp}/ort.tgz" \
    "https://github.com/microsoft/onnxruntime/releases/download/v${ORT_VERSION}/onnxruntime-linux-${ORT_ARCH}-${ORT_VERSION}.tgz"
  tar -xzf "${tmp}/ort.tgz" -C "${tmp}"
  cp "${tmp}/onnxruntime-linux-${ORT_ARCH}-${ORT_VERSION}/lib/libonnxruntime.so.${ORT_VERSION}" "${ORT_LIB}"
fi

for f in onnx/vision_model_quantized.onnx onnx/text_model_quantized.onnx vocab.json merges.txt; do
  out="${DEST}/clip-vit-base-patch32/$(basename "$f")"
  if [[ ! -f "${out}" ]]; then
    echo "Downloading ${f}"
    curl -fL --progress-bar -o "${out}" "${HF_BASE}/${f}"
  fi
done

echo "Models available in ${DEST}"
