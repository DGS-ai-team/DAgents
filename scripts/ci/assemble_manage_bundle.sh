#!/usr/bin/env bash
# 组装 Manage 离线发布包：镜像 tar.gz + docker-compose + 导入/重启脚本。
#
# 用法（仓库根）：
#   VERSION=0.3.7 scripts/ci/assemble_manage_bundle.sh
#   SKIP_BUILD=1 VERSION=0.3.7 scripts/ci/assemble_manage_bundle.sh   # 已有 dist/dagents-manage-VERSION.tar.gz
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${REPO_ROOT}"

VERSION="${VERSION:-dev}"
SKIP_BUILD="${SKIP_BUILD:-0}"
BUNDLE_NAME="dagents-manage-bundle-${VERSION}"
BUNDLE_DIR="${REPO_ROOT}/dist/${BUNDLE_NAME}"
IMAGE_TAR="${REPO_ROOT}/dist/dagents-manage-${VERSION}.tar.gz"
ARCHIVE="${REPO_ROOT}/dist/${BUNDLE_NAME}.tar.gz"

if [[ "${SKIP_BUILD}" != "1" ]]; then
  bash "${SCRIPT_DIR}/build_manage_docker.sh"
fi

[[ -f "${IMAGE_TAR}" ]] || {
  echo "[assemble-manage] missing image: ${IMAGE_TAR}" >&2
  exit 1
}

rm -rf "${BUNDLE_DIR}"
mkdir -p "${BUNDLE_DIR}/image" "${BUNDLE_DIR}/scripts"

cp "${IMAGE_TAR}" "${BUNDLE_DIR}/image/"
cp "${REPO_ROOT}/packaging/manage/docker-compose.offline.yml" "${BUNDLE_DIR}/docker-compose.yml"
cp "${REPO_ROOT}/packaging/manage/.env.example" "${BUNDLE_DIR}/.env.example"

# 写入与镜像 tag 一致的 .env.example
sed -i "s/^MANAGE_VERSION=.*/MANAGE_VERSION=${VERSION}/" "${BUNDLE_DIR}/.env.example"
sed -i "s/^MANAGE_IMAGE=.*/MANAGE_IMAGE=dagents-manage:${VERSION}/" "${BUNDLE_DIR}/.env.example"

cp "${REPO_ROOT}/packaging/manage/scripts/import-image.sh" "${BUNDLE_DIR}/scripts/"
cp "${REPO_ROOT}/packaging/manage/scripts/restart.sh" "${BUNDLE_DIR}/scripts/"
cp "${REPO_ROOT}/packaging/manage/scripts/import-image.bat" "${BUNDLE_DIR}/scripts/"
cp "${REPO_ROOT}/packaging/manage/scripts/restart.bat" "${BUNDLE_DIR}/scripts/"
chmod +x "${BUNDLE_DIR}/scripts/"*.sh

echo "${VERSION}" > "${BUNDLE_DIR}/VERSION"

cat > "${BUNDLE_DIR}/README.txt" <<EOF
DAgents Manage 离线包 (${VERSION})

1. 解压到目标机（需已安装 Docker）
2. 导入镜像：
     Linux/macOS:  bash scripts/import-image.sh
     Windows:      scripts\\import-image.bat
3. 启动 / 重启：
     Linux/macOS:  bash scripts/restart.sh
     Windows:      scripts\\restart.bat

验证:
  curl -sf http://127.0.0.1:8020/health
  浏览器: http://<主机>:8020/console/

数据持久化在 Docker volume manage-data。
升级: 重新 import 新镜像后执行 restart.sh（保留 volume 即可）。

详见 packaging/manage/README.md
EOF

rm -f "${ARCHIVE}"
tar -C "${REPO_ROOT}/dist" -czf "${ARCHIVE}" "$(basename "${BUNDLE_DIR}")"
echo "[assemble-manage] ${ARCHIVE}"
ls -lh "${ARCHIVE}"
