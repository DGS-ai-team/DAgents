#!/usr/bin/env bash
# PyInstaller 打包 Textual TUI（dagents-cli 单文件）。
#
# 用法（仓库根目录，已安装 Python 3.11+ 与 requirements.txt）：
#   scripts/ci/build_dagents_cli.sh
#   GOOS=windows scripts/ci/build_dagents_cli.sh   # 仅影响输出名提示
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

cd "${REPO_ROOT}"
python -m pip install --upgrade pip
python -m pip install -r requirements.txt pyinstaller

CLI_PI_ARGS="${CLI_PI_ARGS:---onefile --name dagents-cli --hidden-import=textual --hidden-import=aiohttp run_dagents_cli.py}"
eval python -m PyInstaller ${CLI_PI_ARGS}

echo "[done] ${REPO_ROOT}/dist/dagents-cli*"
