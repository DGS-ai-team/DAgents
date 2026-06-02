#!/usr/bin/env bash
# 打包本地助手：Go dagents-node + PyInstaller dagents-cli（当前操作系统/架构）。
#
# 用法（仓库根目录）：
#   scripts/package_local_assistant.sh
#   VERSION=0.2.1 scripts/package_local_assistant.sh
#
# 跨平台（linux + windows）请在对应 OS 上分别执行，或使用 CI Release workflow。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
VERSION="${VERSION:-0.2.0}"

case "$(uname -s)" in
  Linux)
    PLATFORM="linux-amd64"
    GOOS=linux
    GOARCH=amd64
    ;;
  MINGW*|MSYS*|CYGWIN*|Windows_NT)
    PLATFORM="windows-amd64"
    GOOS=windows
    GOARCH=amd64
    ;;
  *)
    echo "[error] unsupported OS for local packaging: $(uname -s)" >&2
    exit 1
    ;;
esac

OUT_DIR="${REPO_ROOT}/dist/go-build-${PLATFORM}"
BUILD_CLIENT=1 OUT_DIR="${OUT_DIR}" GOOS="${GOOS}" GOARCH="${GOARCH}" \
  bash "${REPO_ROOT}/scripts/ci/build_go_static.sh"

EXE=""
if [[ "${GOOS}" == "windows" ]]; then
  EXE=".exe"
fi
mkdir -p "${REPO_ROOT}/dist"
cp "${OUT_DIR}/bin/dagents-node${EXE}" "${REPO_ROOT}/dist/dagents-node${EXE}"
cp "${OUT_DIR}/bin/dagents-client${EXE}" "${REPO_ROOT}/dist/dagents-client${EXE}"

bash "${REPO_ROOT}/scripts/ci/build_dagents_cli.sh"

PLATFORM="${PLATFORM}" VERSION="${VERSION}" \
  bash "${REPO_ROOT}/scripts/ci/assemble_local_assistant_bundle.sh"
