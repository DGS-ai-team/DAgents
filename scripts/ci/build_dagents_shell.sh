#!/usr/bin/env bash
# 编译 Windows Desktop Shell（dagents-shell.exe）。
#
# 自 Tauri cutover 起：本脚本委托 build_dagents_shell_tauri.sh，
# 不再编译 Go desktop/tray。
#
# 用法（仓库根目录，须在 Windows）：
#   scripts/ci/build_dagents_shell.sh
#   OUT_DIR=dist scripts/ci/build_dagents_shell.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec bash "${SCRIPT_DIR}/build_dagents_shell_tauri.sh" "$@"
