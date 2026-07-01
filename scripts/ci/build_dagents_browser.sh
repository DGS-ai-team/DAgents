#!/usr/bin/env bash
# PyInstaller 打包 browser-service（dagents-browser 单文件）。
#
# 用法（仓库根目录）：
#   scripts/ci/build_dagents_browser.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

cd "${REPO_ROOT}"

python -m pip install --upgrade pip
python -m pip install -r browser-service/requirements.txt pyinstaller

if [[ -z "${BROWSER_PI_ARGS:-}" ]]; then
  BROWSER_PI_ARGS='--onefile --name dagents-browser --paths browser-service --collect-submodules browser_use --collect-submodules uvicorn --collect-submodules fastapi --collect-submodules pydantic --hidden-import=dagents_browser --hidden-import=dagents_browser.main --hidden-import=dagents_browser.server --hidden-import=dagents_browser.driver --hidden-import=dagents_browser.action_runner --hidden-import=uvicorn.logging --hidden-import=uvicorn.loops --hidden-import=uvicorn.loops.auto --hidden-import=uvicorn.protocols --hidden-import=uvicorn.protocols.http --hidden-import=uvicorn.protocols.http.auto --hidden-import=uvicorn.lifespan --hidden-import=uvicorn.lifespan.on browser-service/run_dagents_browser.py'
fi

# shellcheck disable=SC2086
python -m PyInstaller ${BROWSER_PI_ARGS}

echo "[done] ${REPO_ROOT}/dist/dagents-browser*"
