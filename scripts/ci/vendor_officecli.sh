#!/usr/bin/env bash
# 将 OfficeCLI 二进制与 skills 打入 **Windows** 发布包 .runtime/（assemble 阶段调用）。
#
# 用法：
#   PLATFORM=windows-amd64 BUNDLE_DIR=dist/dagents-local-assistant-windows-amd64 scripts/ci/vendor_officecli.sh
#
# 环境变量：
#   OFFICECLI_VERSION   默认 v1.0.106（与 iOfficeAI/OfficeCLI Release 对齐）
#   OFFICECLI_SKIP=1    跳过（无网络或本地调试）
#   PLATFORM            仅 windows-amd64（Linux 发布包不含 OfficeCLI）
#   BUNDLE_DIR          发布包根目录（含 .runtime/）
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

OFFICECLI_REPO="iOfficeAI/OfficeCLI"
OFFICECLI_VERSION="${OFFICECLI_VERSION:-v1.0.106}"
PLATFORM="${PLATFORM:-linux-amd64}"
BUNDLE_DIR="${BUNDLE_DIR:-}"

if [[ "${OFFICECLI_SKIP:-}" == "1" ]]; then
  echo "[vendor_officecli] skipped (OFFICECLI_SKIP=1)"
  exit 0
fi

if [[ "${PLATFORM}" != windows-* ]]; then
  echo "[vendor_officecli] skipped (OfficeCLI bundled on Windows only, PLATFORM=${PLATFORM})"
  exit 0
fi

if [[ -z "${BUNDLE_DIR}" ]]; then
  echo "[vendor_officecli] BUNDLE_DIR is required" >&2
  exit 1
fi

RUNTIME_DIR="${BUNDLE_DIR}/.runtime"
SCRIPTS_DIR="${RUNTIME_DIR}/scripts"
SKILLS_DIR="${RUNTIME_DIR}/skills"
mkdir -p "${SCRIPTS_DIR}" "${SKILLS_DIR}"

case "${PLATFORM}" in
  windows-*)
    ASSET="officecli-win-x64.exe"
    BIN_NAME="officecli.exe"
    ;;
  *)
    echo "[vendor_officecli] unsupported PLATFORM=${PLATFORM}" >&2
    exit 1
    ;;
esac

MIRROR_URL="https://d.officecli.ai/releases/latest/download/${ASSET}"
GITHUB_URL="https://github.com/${OFFICECLI_REPO}/releases/download/${OFFICECLI_VERSION}/${ASSET}"
TMP_BIN="$(mktemp)"
TMP_ARCHIVE="$(mktemp)"
cleanup() {
  rm -f "${TMP_BIN}" "${TMP_ARCHIVE}"
  rm -rf "${TMP_WORK:-}"
}
trap cleanup EXIT

echo "[vendor_officecli] downloading ${ASSET} (${OFFICECLI_VERSION})..."
if curl -fsSL --connect-timeout 10 --max-time 600 "${MIRROR_URL}" -o "${TMP_BIN}" 2>/dev/null; then
  echo "[vendor_officecli] binary via mirror"
elif curl -fsSL --max-time 600 "${GITHUB_URL}" -o "${TMP_BIN}"; then
  echo "[vendor_officecli] binary via github"
else
  echo "[vendor_officecli] failed to download ${ASSET}" >&2
  exit 1
fi

install -m 0755 "${TMP_BIN}" "${SCRIPTS_DIR}/${BIN_NAME}"
echo "${OFFICECLI_VERSION}" > "${SCRIPTS_DIR}/.officecli-version"

TMP_WORK="$(mktemp -d)"
ARCHIVE_URL="https://github.com/${OFFICECLI_REPO}/archive/refs/tags/${OFFICECLI_VERSION}.tar.gz"

echo "[vendor_officecli] fetching skills from ${ARCHIVE_URL}..."
curl -fsSL "${ARCHIVE_URL}" -o "${TMP_ARCHIVE}"
ARCHIVE_TOP="$(tar -tzf "${TMP_ARCHIVE}" | head -1 | cut -d/ -f1 || true)"
if [[ -z "${ARCHIVE_TOP}" ]]; then
  echo "[vendor_officecli] failed to inspect skills archive" >&2
  exit 1
fi

# skills/officecli/SKILL.md 为 symlink -> ../../SKILL.md；须一并解压根 SKILL.md，
# 且复制时用 cp -L 展开 symlink（Windows CI / zip 分发不接受断链）。
tar -xzf "${TMP_ARCHIVE}" -C "${TMP_WORK}" \
  "${ARCHIVE_TOP}/skills" \
  "${ARCHIVE_TOP}/SKILL.md"

shopt -s nullglob
for skill_dir in "${TMP_WORK}/${ARCHIVE_TOP}/skills"/*; do
  name="$(basename "${skill_dir}")"
  rm -rf "${SKILLS_DIR}/${name}"
  cp -RL "${skill_dir}" "${SKILLS_DIR}/${name}"
  echo "[vendor_officecli] skill ${name}"
done
shopt -u nullglob

echo "[vendor_officecli] done: ${SCRIPTS_DIR}/${BIN_NAME} + skills -> ${SKILLS_DIR}"
