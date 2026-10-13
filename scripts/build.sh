#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
OUTPUT_DIR="${ROOT_DIR}/dist"
BINARY_NAME="sentinel"

mkdir -p "${OUTPUT_DIR}"

echo "Building standalone stripped static binary (${BINARY_NAME})..."
CGO_ENABLED=0 go build \
    -ldflags="-s -w -X main.version=1.0.0" \
    -trimpath \
    -o "${OUTPUT_DIR}/${BINARY_NAME}" \
    "${ROOT_DIR}/cmd/sentinel"

echo "Build complete: ${OUTPUT_DIR}/${BINARY_NAME}"
ls -lh "${OUTPUT_DIR}/${BINARY_NAME}"
