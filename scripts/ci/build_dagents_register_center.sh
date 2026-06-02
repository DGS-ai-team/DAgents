#!/usr/bin/env bash
# PyInstaller 打包 Register Center（dagents_register_center 单文件）。
#
# 用法（仓库根目录）：
#   scripts/ci/build_dagents_register_center.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

cd "${REPO_ROOT}"
python -m pip install --upgrade pip
python -m pip install -r requirements.txt pyinstaller

ADD_DATA="register_center:register_center"
case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*|Windows_NT) ADD_DATA="register_center;register_center" ;;
esac

RC_PI_ARGS="${RC_PI_ARGS:---onefile --name dagents_register_center --add-data ${ADD_DATA} run_register_center.py}"
eval python -m PyInstaller ${RC_PI_ARGS}

echo "[done] ${REPO_ROOT}/dist/dagents_register_center*"
