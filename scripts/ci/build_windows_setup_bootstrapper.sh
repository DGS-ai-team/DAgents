#!/usr/bin/env bash
# CI：在已有 Inno 安装包基础上构建 Tauri「DAgents Setup」向导（与 Inno 裸包并存发布）。
# 前置：dist/dagents-local-assistant-windows-amd64-installer-*.exe
# 用法（仓库根，Windows runner）：
#   VERSION=0.8.4 bash scripts/ci/build_windows_setup_bootstrapper.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${REPO_ROOT}"

VERSION="${VERSION:-0.0.0}"
VERSION="${VERSION#v}"

chmod +x packaging/bootstrapper/scripts/package-with-inno.sh
OUT_DIR="${OUT_DIR:-${REPO_ROOT}/dist}" VERSION="${VERSION}" \
  bash packaging/bootstrapper/scripts/package-with-inno.sh "${VERSION}"

test -f "dist/dagents-setup-windows-amd64-${VERSION}.exe"
ls -lh "dist/dagents-setup-windows-amd64-${VERSION}.exe"
