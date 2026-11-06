#!/usr/bin/env bash
# Downloads the ONNX Runtime shared library and the CLIP ONNX models used by
# internal/embedding into phalbum/models (git-ignored): OpenAI CLIP ViT-B/32 for
# images and English text, and a multilingual text encoder distilled into the
# same embedding space.
set -euo pipefail

ORT_VERSION="1.31.0" # must match the C API version used by onnxruntime_go
HF_REPO="Xenova/clip-vit-base-patch32"
HF_BASE="https://huggingface.co/${HF_REPO}/resolve/main"
MCLIP_REPO="sentence-transformers/clip-ViT-B-32-multilingual-v1"
MCLIP_BASE="https://huggingface.co/${MCLIP_REPO}/resolve/main"

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEST="${ROOT}/models"
mkdir -p "${DEST}/clip-vit-base-patch32" "${DEST}/clip-ViT-B-32-multilingual-v1"

case "$(uname -m)" in
  x86_64) ORT_ARCH="x64"; MCLIP_ONNX="model_quint8_avx2.onnx" ;;
  aarch64 | arm64) ORT_ARCH="aarch64"; MCLIP_ONNX="model_qint8_arm64.onnx" ;;
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

# The ONNX file holds only the transformer; mean pooling and the Dense
# projection to 512 dimensions run in Go.
fetch_mclip() {
  local out="${DEST}/clip-ViT-B-32-multilingual-v1/$2"
  if [[ ! -f "${out}" ]]; then
    echo "Downloading ${MCLIP_REPO} $1"
    curl -fL --progress-bar -o "${out}" "${MCLIP_BASE}/$1"
  fi
}
fetch_mclip "onnx/${MCLIP_ONNX}" text_model_quantized.onnx
fetch_mclip vocab.txt vocab.txt
fetch_mclip 2_Dense/model.safetensors dense.safetensors

echo "Models available in ${DEST}"
