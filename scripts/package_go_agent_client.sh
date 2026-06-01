#!/usr/bin/env bash
# 打包 Go Agent Node + Client 发布物（linux tarball + windows zip）。
#
# 用法（仓库根目录）：
#   scripts/package_go_agent_client.sh
#   VERSION=0.2.1 scripts/package_go_agent_client.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
BUILD="${REPO_ROOT}/scripts/ci/build_go_static.sh"
VERSION="${VERSION:-0.2.0}"

LINUX_DIR="${REPO_ROOT}/dist/dagents-agent-client-linux-amd64"
WIN_DIR="${REPO_ROOT}/dist/dagents-agent-client-windows-amd64"
LINUX_TAR="${REPO_ROOT}/dist/dagents-agent-client-linux-amd64-${VERSION}.tar.gz"
WIN_ZIP="${REPO_ROOT}/dist/dagents-agent-client-windows-amd64-${VERSION}.zip"

OUT_DIR="${LINUX_DIR}" GOOS=linux GOARCH=amd64 bash "${BUILD}"
OUT_DIR="${WIN_DIR}" GOOS=windows GOARCH=amd64 bash "${BUILD}"

mkdir -p "${REPO_ROOT}/dist"
tar -C "${REPO_ROOT}/dist" -czf "${LINUX_TAR}" "$(basename "${LINUX_DIR}")"

if command -v zip >/dev/null 2>&1; then
  (cd "${REPO_ROOT}/dist" && zip -rq "$(basename "${WIN_ZIP}")" "$(basename "${WIN_DIR}")")
else
  (cd "${REPO_ROOT}/dist" && tar -czf "${WIN_ZIP%.zip}.tar.gz" "$(basename "${WIN_DIR}")")
  echo "[warn] zip not found; wrote ${WIN_ZIP%.zip}.tar.gz instead"
fi

echo "[done] ${LINUX_TAR}"
if [[ -f "${WIN_ZIP}" ]]; then
  echo "[done] ${WIN_ZIP}"
fi
