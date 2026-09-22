#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(
  cd "$(dirname "${BASH_SOURCE[0]}")/.." &&
    pwd
)"

DIST_DIR="${DIST_DIR:-${ROOT_DIR}/dist}"

VERSION="${VERSION:-$(git -C "${ROOT_DIR}" describe --tags --always --dirty 2>/dev/null || echo dev)}"
COMMIT="${COMMIT:-$(git -C "${ROOT_DIR}" rev-parse --short HEAD 2>/dev/null || echo unknown)}"
BUILD_DATE="${BUILD_DATE:-$(date -u '+%Y-%m-%dT%H:%M:%SZ')}"

LDFLAGS="
-X main.version=${VERSION}
-X main.commit=${COMMIT}
-X main.buildDate=${BUILD_DATE}
"

TARGETS=(
  "linux amd64"
  "linux arm64"
  "darwin amd64"
  "darwin arm64"
)

mkdir -p "${DIST_DIR}"

rm -f \
  "${DIST_DIR}"/kvoctl-* \
  "${DIST_DIR}/SHA256SUMS"

echo "Building kvoctl"
echo "  Version:    ${VERSION}"
echo "  Commit:     ${COMMIT}"
echo "  Build date: ${BUILD_DATE}"
echo "  Output:     ${DIST_DIR}"
echo

for target in "${TARGETS[@]}"; do
  read -r GOOS GOARCH <<< "${target}"

  OUTPUT="${DIST_DIR}/kvoctl-${GOOS}-${GOARCH}"

  echo "Building ${GOOS}/${GOARCH} -> ${OUTPUT}"

  (
    cd "${ROOT_DIR}"

    CGO_ENABLED=0 \
    GOOS="${GOOS}" \
    GOARCH="${GOARCH}" \
      go build \
        -trimpath \
        -ldflags="${LDFLAGS}" \
        -o "${OUTPUT}" \
        ./cmd/kvoctl
  )
done

echo
echo "Generating SHA256 checksums..."

(
  cd "${DIST_DIR}"

  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum kvoctl-* > SHA256SUMS
  else
    shasum -a 256 kvoctl-* > SHA256SUMS
  fi
)

echo
echo "Build complete:"
ls -lh "${DIST_DIR}"/kvoctl-* "${DIST_DIR}/SHA256SUMS"
