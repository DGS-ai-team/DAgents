#!/usr/bin/env bash
# 将已构建的 Inno 安装包嵌入 Tauri Setup 并打包。
# 用法（仓库根）：bash packaging/bootstrapper/scripts/package-with-inno.sh [version]
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
VERSION="${1:-${VERSION:-0.0.0}}"
BOOT="${ROOT}/packaging/bootstrapper"
RES="${BOOT}/src-tauri/resources"
mkdir -p "${RES}"

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

# 同步版本号到 tauri.conf / package.json（粗替换）
python3 - <<PY
import json, pathlib, re
ver = "${VERSION}".lstrip("v")
pkg = pathlib.Path("package.json")
data = json.loads(pkg.read_text())
data["version"] = ver
pkg.write_text(json.dumps(data, indent=2) + "\n")
conf = pathlib.Path("src-tauri/tauri.conf.json")
text = conf.read_text()
text = re.sub(r'"version"\s*:\s*"[^"]*"', f'"version": "{ver}"', text, count=1)
conf.write_text(text)
cargo = pathlib.Path("src-tauri/Cargo.toml")
c = cargo.read_text()
c = re.sub(r'(?m)^version = "[^"]*"', f'version = "{ver}"', c, count=1)
cargo.write_text(c)
print(f"[bootstrapper] version -> {ver}")
PY

npm run tauri build
echo "[bootstrapper] done. 查看 ${BOOT}/src-tauri/target/release/bundle/"
