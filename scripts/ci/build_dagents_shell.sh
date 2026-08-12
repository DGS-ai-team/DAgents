#!/usr/bin/env bash
# 编译 Windows Desktop Shell — Go 兼容版（dagents-shell-legacy.exe）。
#
# 用于低版本 Windows / 无 WebView2 环境；安装包「兼容模式」选项。
# 推荐轨为 Tauri：scripts/ci/build_dagents_shell_tauri.sh
#
# 用法（仓库根目录，须在 Windows 或具备 mingw 的交叉环境）：
#   scripts/ci/build_dagents_shell.sh
#   OUT_DIR=dist GOOS=windows GOARCH=amd64 scripts/ci/build_dagents_shell.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

OUT_DIR="${OUT_DIR:-${REPO_ROOT}/dist}"
GOOS="${GOOS:-windows}"
GOARCH="${GOARCH:-amd64}"
LDFLAGS="${LDFLAGS:--s -w}"

if [[ "${GOOS}" != "windows" ]]; then
  echo "[build] dagents-shell-legacy is Windows-only; skipping (GOOS=${GOOS})" >&2
  exit 0
fi

EXE=".exe"
mkdir -p "${OUT_DIR}"

echo "[build] ${GOOS}/${GOARCH} desktop/tray -> ${OUT_DIR}/dagents-shell-legacy${EXE}"
(
  cd "${REPO_ROOT}/desktop/tray"
  CGO_ENABLED=1 GOOS="${GOOS}" GOARCH="${GOARCH}" \
    go build -ldflags="${LDFLAGS}" -o "${OUT_DIR}/dagents-shell-legacy${EXE}" .
)

echo "[done] ${OUT_DIR}/dagents-shell-legacy${EXE}"
ls -la "${OUT_DIR}/dagents-shell-legacy${EXE}"
