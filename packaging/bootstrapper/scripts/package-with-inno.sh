#!/usr/bin/env bash
# 将已构建的 Inno 安装包嵌入 Tauri Setup 并打包。
# 用法（仓库根）：bash packaging/bootstrapper/scripts/package-with-inno.sh [version]
#
# 产物（并存发布）：
#   dist/dagents-setup-windows-amd64-{VERSION}.exe  — Tauri NSIS 外层向导（内嵌 Inno）
# Inno 裸包仍由 scripts/ci/build_windows_installer.sh 产出，二者一并发布。
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
VERSION="${1:-${VERSION:-0.0.0}}"
VERSION="${VERSION#v}"
BOOT="${ROOT}/packaging/bootstrapper"
RES="${BOOT}/src-tauri/resources"
OUT_DIR="${OUT_DIR:-${ROOT}/dist}"
OUT_NAME="dagents-setup-windows-amd64-${VERSION}.exe"
mkdir -p "${RES}" "${OUT_DIR}"

PAYLOAD="$(ls -1 "${ROOT}/dist"/dagents-local-assistant-windows-amd64-installer-*.exe 2>/dev/null | sort | tail -n 1 || true)"
if [[ -z "${PAYLOAD}" ]]; then
  echo "error: 未找到 dist/dagents-local-assistant-windows-amd64-installer-*.exe" >&2
  echo "请先运行 assemble + build_windows_installer.sh" >&2
  exit 1
fi

rm -f "${RES}"/dagents-local-assistant-windows-amd64-installer-*.exe
cp -f "${PAYLOAD}" "${RES}/"
echo "[bootstrapper] embedded payload: $(basename "${PAYLOAD}")"

cd "${BOOT}"
if [[ ! -d node_modules ]]; then
  npm ci || npm install
fi

PYTHON=python3
if ! command -v python3 >/dev/null 2>&1; then
  PYTHON=python
fi

# 同步版本号到 tauri.conf / package.json / Cargo.toml
"${PYTHON}" - <<PY
import json, pathlib, re
ver = "${VERSION}".lstrip("v")
pkg = pathlib.Path("package.json")
data = json.loads(pkg.read_text(encoding="utf-8"))
data["version"] = ver
pkg.write_text(json.dumps(data, indent=2) + "\n", encoding="utf-8")
conf = pathlib.Path("src-tauri/tauri.conf.json")
text = conf.read_text(encoding="utf-8")
text = re.sub(r'"version"\s*:\s*"[^"]*"', f'"version": "{ver}"', text, count=1)
conf.write_text(text, encoding="utf-8")
cargo = pathlib.Path("src-tauri/Cargo.toml")
c = cargo.read_text(encoding="utf-8")
c = re.sub(r'(?m)^version = "[^"]*"', f'version = "{ver}"', c, count=1)
cargo.write_text(c, encoding="utf-8")
print(f"[bootstrapper] version -> {ver}")
PY

npm run tauri build

BUNDLE_NSIS="${BOOT}/src-tauri/target/release/bundle/nsis"
SETUP_SRC="$(ls -1 "${BUNDLE_NSIS}"/*setup.exe 2>/dev/null | sort | tail -n 1 || true)"
if [[ -z "${SETUP_SRC}" ]]; then
  SETUP_SRC="$(ls -1 "${BUNDLE_NSIS}"/*.exe 2>/dev/null | sort | tail -n 1 || true)"
fi
if [[ -z "${SETUP_SRC}" || ! -f "${SETUP_SRC}" ]]; then
  echo "error: Tauri NSIS 产物未找到（${BUNDLE_NSIS}）" >&2
  ls -la "${BUNDLE_NSIS}" 2>/dev/null || true
  ls -laR "${BOOT}/src-tauri/target/release/bundle" 2>/dev/null || true
  exit 1
fi

cp -f "${SETUP_SRC}" "${OUT_DIR}/${OUT_NAME}"
echo "[bootstrapper] ${OUT_DIR}/${OUT_NAME}"
ls -lh "${OUT_DIR}/${OUT_NAME}"
