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

# The current browser lock uses the newest cryptography release for the
# normal x64/Linux builds.  Windows x86 does not receive a win32 wheel for
# that release, so the x86 release job can provide the latest compatible
# wheel through BROWSER_CRYPTOGRAPHY_VERSION without duplicating the lock.
BROWSER_REQUIREMENTS_FILE="${BROWSER_REQUIREMENTS_FILE:-browser-service/requirements.lock}"
BROWSER_REQUIREMENTS_TEMP=""
cleanup_browser_requirements() {
  if [[ -n "${BROWSER_REQUIREMENTS_TEMP}" ]]; then
    rm -f -- "${BROWSER_REQUIREMENTS_TEMP}"
  fi
}
trap cleanup_browser_requirements EXIT

if [[ -n "${BROWSER_CRYPTOGRAPHY_VERSION:-}" ]]; then
  BROWSER_REQUIREMENTS_TEMP="$(mktemp)"
  awk -v version="${BROWSER_CRYPTOGRAPHY_VERSION}" '
    /^cryptography==/ {
      print "cryptography==" version
      replaced = 1
      next
    }
    { print }
    END {
      if (!replaced) {
        exit 1
      }
    }
  ' "${BROWSER_REQUIREMENTS_FILE}" > "${BROWSER_REQUIREMENTS_TEMP}"
  BROWSER_REQUIREMENTS_FILE="${BROWSER_REQUIREMENTS_TEMP}"
fi

python -m pip install -r "${BROWSER_REQUIREMENTS_FILE}" pyinstaller

if [[ -z "${BROWSER_PI_ARGS:-}" ]]; then
  BROWSER_PI_ARGS='--onefile --name dagents-browser --paths browser-service --collect-submodules browser_use --collect-submodules uvicorn --collect-submodules fastapi --collect-submodules pydantic --hidden-import=dagents_browser --hidden-import=dagents_browser.main --hidden-import=dagents_browser.server --hidden-import=dagents_browser.driver --hidden-import=uvicorn.logging --hidden-import=uvicorn.loops --hidden-import=uvicorn.loops.auto --hidden-import=uvicorn.protocols --hidden-import=uvicorn.protocols.http --hidden-import=uvicorn.protocols.http.auto --hidden-import=uvicorn.lifespan --hidden-import=uvicorn.lifespan.on browser-service/run_dagents_browser.py'
fi

# shellcheck disable=SC2086
python -m PyInstaller ${BROWSER_PI_ARGS}

echo "[done] ${REPO_ROOT}/dist/dagents-browser*"
