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
#   INSTALLER_QA=1 仅编译隔离 QA 包（独立 AppId；不创建快捷方式、自启或 PATH）
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

VERSION="${VERSION:-}"
if [[ -z "${VERSION}" ]]; then
  echo "[installer] VERSION is required; pass the release/package version explicitly" >&2
  exit 1
fi
source_version="$(tr -d '\r\n' < "${REPO_ROOT}/VERSION")"
if [[ "${VERSION}" != "${source_version}" ]]; then
  echo "[installer] VERSION mismatch: requested ${VERSION}, source VERSION contains ${source_version}" >&2
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
INSTALLER_QA="${INSTALLER_QA:-0}"
PACKAGE_VERSION="${VERSION}"
PACKAGE_VERSION_KIND="bundle"
case "${INSTALLER_QA}" in
  0)
    if [[ -n "${QA_PACKAGE_VERSION:-}" ]]; then
      echo "[installer] QA_PACKAGE_VERSION is allowed only with INSTALLER_QA=1" >&2
      exit 1
    fi
    ;;
  1)
    if [[ -n "${QA_PACKAGE_VERSION:-}" ]]; then
      qa_base="${QA_PACKAGE_VERSION%.*}"
      qa_revision="${QA_PACKAGE_VERSION##*.}"
      if [[ ! "${VERSION}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] ||
         [[ ! "${QA_PACKAGE_VERSION}" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[1-9][0-9]*$ ]] ||
         [[ "${qa_base}" != "${VERSION}" ]] || [[ ! "${qa_revision}" =~ ^[1-9][0-9]*$ ]]; then
        echo "[installer] QA_PACKAGE_VERSION must append a positive numeric fourth component to the three-part source VERSION" >&2
        exit 1
      fi
      PACKAGE_VERSION="${QA_PACKAGE_VERSION}"
      PACKAGE_VERSION_KIND="synthetic-qa-sequence"
    fi
    OUTPUT_BASE="dagents-local-assistant-${PLATFORM}-installer-${PACKAGE_VERSION}-qa"
    ;;
  *)
    echo "[installer] INSTALLER_QA must be 0 or 1" >&2
    exit 1
    ;;
esac

if [[ ! -d "${BUNDLE_SRC}" ]]; then
  echo "[installer] missing bundle source dir: ${BUNDLE_SRC}" >&2
  echo "[installer] run assemble_local_assistant_bundle.sh first (PLATFORM=${PLATFORM})" >&2
  exit 1
fi

if [[ ! -s "${BUNDLE_SRC}/bin/dagents-shell.exe" ]]; then
  echo "[installer] missing canonical Desktop binary: ${BUNDLE_SRC}/bin/dagents-shell.exe" >&2
  exit 1
fi
if [[ -e "${BUNDLE_SRC}/bin/dagents-shell-tauri.exe" ]]; then
  echo "[installer] retired tauri-suffixed Desktop alias is not accepted" >&2
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

repo_root_real="$(cd "${REPO_ROOT}" && pwd -P)"
bundle_parent_real="$(cd "$(dirname "${BUNDLE_DIR}")" && pwd -P)"
if [[ "${bundle_parent_real}" != "${repo_root_real}" || "$(basename "${BUNDLE_DIR}")" != "bundle" || -L "${BUNDLE_DIR}" ]]; then
  echo "[installer] refusing to replace an unexpected bundle path: ${BUNDLE_DIR}" >&2
  exit 1
fi
bundle_src_real="$(cd "${BUNDLE_SRC}" && pwd -P)"
bundle_target_real="${repo_root_real}/bundle"
bundle_src_key="$(printf '%s' "${bundle_src_real}" | tr '[:upper:]' '[:lower:]')"
bundle_target_key="$(printf '%s' "${bundle_target_real}" | tr '[:upper:]' '[:lower:]')"
if [[ "${bundle_src_key}" == "${bundle_target_key}" || "${bundle_src_key}" == "${bundle_target_key}/"* ]]; then
  echo "[installer] bundle source must not be inside the staging directory: ${BUNDLE_SRC}" >&2
  exit 1
fi
if [[ ! -f "${BUNDLE_SRC}/VERSION" ]]; then
  echo "[installer] missing bundle version file: ${BUNDLE_SRC}/VERSION" >&2
  exit 1
fi
bundle_version="$(tr -d '\r\n' < "${BUNDLE_SRC}/VERSION")"
if [[ "${bundle_version}" != "${source_version}" || "${bundle_version}" != "${VERSION}" ]]; then
  echo "[installer] VERSION mismatch: source=${source_version}, requested=${VERSION}, bundle=${bundle_version}" >&2
  exit 1
fi
echo "[installer] staging ${BUNDLE_SRC} -> ${BUNDLE_DIR}"
rm -rf "${BUNDLE_DIR}"
cp -a "${BUNDLE_SRC}/." "${BUNDLE_DIR}/"

mkdir -p "${OUTPUT_DIR}"
echo "[installer] compiling ${OUTPUT_BASE}.exe"
COMPILE_LOG="${OUTPUT_DIR}/${OUTPUT_BASE}.compile.log"

# Git Bash (MSYS) 会把 /D... 转成 D:\...，Inno Setup 会误当作第二个脚本路径。
define_args=(
  "//DMyAppVersion=${PACKAGE_VERSION}"
  "//DMyAppArch=${INNO_ARCH}"
  "//DMyOutputBaseFilename=${OUTPUT_BASE}"
)
if [[ "${INSTALLER_QA}" == "1" ]]; then
  define_args+=("//DIsolatedQaBuild=1")
fi
iss_file="${REPO_ROOT}/packaging/windows/dagents-installer.iss"
if command -v cygpath >/dev/null 2>&1; then
  iss_file="$(cygpath -w "${iss_file}")"
fi
"${ISCC}" "${define_args[@]}" "${iss_file}" 2>&1 | tee "${COMPILE_LOG}"

INSTALLER="${OUTPUT_DIR}/${OUTPUT_BASE}.exe"
if [[ ! -f "${INSTALLER}" ]]; then
  echo "[installer] expected output missing: ${INSTALLER}" >&2
  exit 1
fi
if [[ "${INSTALLER_QA}" == "1" ]]; then
  if ! command -v sha256sum >/dev/null 2>&1; then
    echo "[installer] sha256sum is required to mark an isolated QA build" >&2
    exit 1
  fi
  installer_hash="$(sha256sum "${INSTALLER}" | awk '{print $1}')"
  payloads_json=""
  payload_separator=""
  for payload in "${BUNDLE_DIR}/bin/"*; do
    [[ -f "${payload}" ]] || continue
    payload_name="$(basename "${payload}")"
    payload_hash="$(sha256sum "${payload}" | awk '{print $1}')"
    payloads_json+="${payload_separator}{\"name\":\"${payload_name}\",\"sha256\":\"${payload_hash}\"}"
    payload_separator=','
  done
  printf '{"macro":"IsolatedQaBuild=1","appId":"{B5D1A7C3-2E49-4F60-A8B1-7C3D9E5F2046}","version":"%s","packageVersion":"%s","packageVersionKind":"%s","bundleVersion":"%s","installer":"%s","sha256":"%s","payloads":[%s]}\n' \
    "${PACKAGE_VERSION}" "${PACKAGE_VERSION}" "${PACKAGE_VERSION_KIND}" "${bundle_version}" "$(basename "${INSTALLER}")" "${installer_hash}" "${payloads_json}" > "${INSTALLER}.manifest.json"
fi

mkdir -p "${REPO_ROOT}/dist"
cp "${INSTALLER}" "${REPO_ROOT}/dist/"
if [[ "${INSTALLER_QA}" == "1" ]]; then
  cp "${INSTALLER}.manifest.json" "${REPO_ROOT}/dist/"
fi
echo "[done] ${INSTALLER}"
echo "[done] ${REPO_ROOT}/dist/$(basename "${INSTALLER}")"
