#!/usr/bin/env bash
# 将已组装的架构对应 Windows 助手目录 staging 到 bundle/，
# 并用 Inno Setup 生成 Windows 安装包（.exe）。
#
# 用法（仓库根目录，Windows runner 或已安装 Inno Setup 6 的环境）：
#   PLATFORM=windows-amd64 VERSION=0.2.2 scripts/ci/build_windows_installer.sh
#   PLATFORM=windows-386 VERSION=0.2.2 scripts/ci/build_windows_installer.sh
#
# 环境变量：
#   PLATFORM     windows-amd64（x64）或 windows-386（x86）
#   BUNDLE_SRC   源目录（默认 dist/dagents-local-assistant-${PLATFORM}）
#   ISCC         Inno Setup 编译器路径
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

VERSION="${VERSION:-}"
if [[ -z "${VERSION}" ]]; then
  echo "[installer] VERSION is required; pass the release/package version explicitly" >&2
  exit 1
fi
PLATFORM="${PLATFORM:-windows-amd64}"
case "${PLATFORM}" in
  windows-amd64)
    INNO_ARCH="x64"
    ;;
  windows-386)
    INNO_ARCH="x86"
    ;;
  *)
    echo "[installer] unsupported Windows platform: ${PLATFORM} (expected windows-amd64 or windows-386)" >&2
    exit 1
    ;;
esac

BUNDLE_SRC="${BUNDLE_SRC:-${REPO_ROOT}/dist/dagents-local-assistant-${PLATFORM}}"
BUNDLE_DIR="${REPO_ROOT}/bundle"
OUTPUT_DIR="${REPO_ROOT}/dist-installer"
OUTPUT_BASE="dagents-local-assistant-${PLATFORM}-installer-${VERSION}"

if [[ ! -d "${BUNDLE_SRC}" ]]; then
  echo "[installer] missing bundle source dir: ${BUNDLE_SRC}" >&2
  echo "[installer] run assemble_local_assistant_bundle.sh first (PLATFORM=${PLATFORM})" >&2
  exit 1
fi

if [[ -z "${ISCC:-}" ]]; then
  for candidate in \
    "/c/Program Files (x86)/Inno Setup 6/ISCC.exe" \
    "/c/Program Files/Inno Setup 6/ISCC.exe"; do
    if [[ -f "${candidate}" ]]; then
      ISCC="${candidate}"
      break
    fi
  done
fi
if [[ -z "${ISCC:-}" || ! -f "${ISCC}" ]]; then
  echo "[installer] ISCC not found; set ISCC to Inno Setup compiler (ISCC.exe)" >&2
  exit 1
fi

echo "[installer] staging ${BUNDLE_SRC} -> ${BUNDLE_DIR}"
rm -rf "${BUNDLE_DIR}"
cp -a "${BUNDLE_SRC}/." "${BUNDLE_DIR}/"

mkdir -p "${OUTPUT_DIR}"
echo "[installer] compiling ${OUTPUT_BASE}.exe"

# Git Bash (MSYS) 会把 /D... 转成 D:\...，Inno Setup 会误当作第二个脚本路径。
define_args=(
  "//DMyAppVersion=${VERSION}"
  "//DMyAppArch=${INNO_ARCH}"
  "//DMyOutputBaseFilename=${OUTPUT_BASE}"
)
iss_file="${REPO_ROOT}/packaging/windows/dagents-installer.iss"
if command -v cygpath >/dev/null 2>&1; then
  iss_file="$(cygpath -w "${iss_file}")"
fi
"${ISCC}" "${define_args[@]}" "${iss_file}"

INSTALLER="${OUTPUT_DIR}/${OUTPUT_BASE}.exe"
if [[ ! -f "${INSTALLER}" ]]; then
  echo "[installer] expected output missing: ${INSTALLER}" >&2
  exit 1
fi

mkdir -p "${REPO_ROOT}/dist"
cp "${INSTALLER}" "${REPO_ROOT}/dist/"
echo "[done] ${INSTALLER}"
echo "[done] ${REPO_ROOT}/dist/$(basename "${INSTALLER}")"
