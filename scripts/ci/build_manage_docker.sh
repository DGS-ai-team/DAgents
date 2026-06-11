#!/usr/bin/env bash
# 构建 Manage Docker 镜像并导出 tar.gz（Release CI / 本地）
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT}"

VERSION="${VERSION:-dev}"
IMAGE="${MANAGE_IMAGE:-dagents-manage:${VERSION}}"
OUT_DIR="${OUT_DIR:-${ROOT}/dist}"
TAR="${OUT_DIR}/dagents-manage-${VERSION}.tar.gz"

mkdir -p "${OUT_DIR}"

echo "== build ${IMAGE} =="
docker build \
  -f packaging/manage/Dockerfile \
  --build-arg "VERSION=${VERSION}" \
  -t "${IMAGE}" \
  .

echo "== save ${TAR} =="
docker save "${IMAGE}" | gzip -9 > "${TAR}"
ls -lh "${TAR}"
