#!/usr/bin/env bash
# 编译 Tauri 版 Windows Desktop Shell（dagents-shell-tauri.exe）。
#
# 须在 Windows 上运行（MSVC + WebView2；GitHub windows-2022 即可）。
#
# 用法（仓库根目录）：
#   scripts/ci/build_dagents_shell_tauri.sh
#   OUT_DIR=dist scripts/ci/build_dagents_shell_tauri.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
APP_DIR="${REPO_ROOT}/desktop/tray-tauri"

OUT_DIR="${OUT_DIR:-${REPO_ROOT}/dist}"
SKIP_BUNDLE="${SKIP_BUNDLE:-1}"

uname_s="$(uname -s 2>/dev/null || true)"
case "${uname_s}" in
  MINGW*|MSYS*|CYGWIN*|Windows_NT)
    ;;
  *)
    if [[ "${OS:-}" == "Windows_NT" ]]; then
      :
    else
      echo "[build] dagents-shell-tauri is Windows-native; skipping (uname=${uname_s})" >&2
      exit 0
    fi
    ;;
esac

mkdir -p "${OUT_DIR}"

if [[ ! -f "${APP_DIR}/package.json" ]]; then
  echo "[build] missing ${APP_DIR}/package.json" >&2
  exit 1
fi

echo "[build] npm ci (desktop/tray-tauri)"
(
  cd "${APP_DIR}"
  if [[ -f package-lock.json ]]; then
    npm ci
  else
    npm install
  fi
)

BUNDLE_ARGS=()
if [[ "${SKIP_BUNDLE}" == "1" ]]; then
  # 只要 release 可执行文件，跳过 NSIS（CI 更快）
  BUNDLE_ARGS+=(--bundles none)
fi

echo "[build] tauri build ${BUNDLE_ARGS[*]:-}"
(
  cd "${APP_DIR}"
  # 使用本地 CLI，避免全局版本漂移
  npx --no-install tauri build "${BUNDLE_ARGS[@]}"
)

SRC="${APP_DIR}/src-tauri/target/release/dagents-shell.exe"
if [[ ! -f "${SRC}" ]]; then
  echo "[build] expected binary missing: ${SRC}" >&2
  ls -la "${APP_DIR}/src-tauri/target/release/" >&2 || true
  exit 1
fi

DEST="${OUT_DIR}/dagents-shell-tauri.exe"
cp "${SRC}" "${DEST}"
echo "[done] ${DEST}"
# 体积提示
ls -la "${DEST}"
