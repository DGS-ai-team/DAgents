#!/usr/bin/env bash
# 兼容入口：本地助手打包（Go Node + Textual dagents-cli）。
#
# 用法：
#   scripts/package_go_agent_client.sh   # 已废弃命名，委托 package_local_assistant
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
echo "[warn] package_go_agent_client.sh 已合并为 package_local_assistant.sh" >&2
exec bash "${SCRIPT_DIR}/package_local_assistant.sh" "$@"
