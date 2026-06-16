#!/usr/bin/env bash
# 离线导入 Manage Docker 镜像（在解压后的 dagents-manage-bundle-* 根目录执行）。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
IMAGE_DIR="${ROOT}/image"

die() {
  echo "[import-image] error: $*" >&2
  exit 1
}

info() {
  echo "[import-image] $*"
}

if ! command -v docker >/dev/null 2>&1; then
  die "docker not found; install Docker first"
fi

VERSION=""
if [[ -f "${ROOT}/VERSION" ]]; then
  VERSION="$(tr -d '[:space:]' <"${ROOT}/VERSION")"
fi

IMAGE_TAR=""
if [[ -n "${VERSION}" && -f "${IMAGE_DIR}/dagents-manage-${VERSION}.tar.gz" ]]; then
  IMAGE_TAR="${IMAGE_DIR}/dagents-manage-${VERSION}.tar.gz"
else
  shopt -s nullglob
  candidates=("${IMAGE_DIR}"/dagents-manage-*.tar.gz)
  shopt -u nullglob
  if [[ ${#candidates[@]} -eq 1 ]]; then
    IMAGE_TAR="${candidates[0]}"
  elif [[ ${#candidates[@]} -gt 1 ]]; then
    die "multiple image archives in ${IMAGE_DIR}; set VERSION or remove extras"
  fi
fi

[[ -n "${IMAGE_TAR}" && -f "${IMAGE_TAR}" ]] || die "missing image archive under ${IMAGE_DIR}/"

info "loading ${IMAGE_TAR}"
docker load -i "${IMAGE_TAR}"
info "done. verify: docker image ls dagents-manage"
info "next: bash scripts/restart.sh"
