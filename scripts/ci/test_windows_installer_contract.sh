#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd -P)"
TEST_ROOT="$(mktemp -d "${REPO_ROOT}/.runtime-test-installer-contract.XXXXXX")"
WORKSPACE_REAL="$(cd "${REPO_ROOT}" && pwd -P)"

cleanup() {
  case "${TEST_ROOT}" in
    "${WORKSPACE_REAL}"/.runtime-test-installer-contract.*)
      rm -rf -- "${TEST_ROOT}"
      ;;
    *)
      echo "[test] refusing to remove unexpected fixture path: ${TEST_ROOT}" >&2
      return 1
      ;;
  esac
}
trap cleanup EXIT

fail() {
  echo "[test] $*" >&2
  exit 1
}

fixture="${TEST_ROOT}/repo"
source_dir="${TEST_ROOT}/source"
mkdir -p "${fixture}/scripts/ci" "${fixture}/bundle" "${source_dir}"
cp "${SCRIPT_DIR}/build_windows_installer.sh" "${fixture}/scripts/ci/build_windows_installer.sh"
printf '#!/bin/sh\nexit 0\n' > "${TEST_ROOT}/ISCC.exe"
chmod +x "${TEST_ROOT}/ISCC.exe"
printf '0.11.0-test\n' > "${source_dir}/VERSION"
printf 'preserve installer staging\n' > "${fixture}/bundle/sentinel.txt"

run_installer() {
  local source="${BUNDLE_SRC_OVERRIDE:-${source_dir}}"
  PLATFORM=windows-amd64 \
  VERSION=0.11.0-test \
  BUNDLE_SRC="${source}" \
  ISCC="${TEST_ROOT}/ISCC.exe" \
    bash "${fixture}/scripts/ci/build_windows_installer.sh"
}

expect_rejected_without_staging_change() {
  local name="$1"
  local expected="$2"
  local output status=0
  output="$(run_installer 2>&1)" || status=$?
  [[ "${status}" -ne 0 ]] || fail "${name}: installer unexpectedly accepted the bundle"
  [[ "${output}" == *"${expected}"* ]] || fail "${name}: expected diagnostic was missing"
  [[ "$(cat "${fixture}/bundle/sentinel.txt")" == "preserve installer staging" ]] || fail "${name}: old installer staging changed"
}

mkdir -p "${source_dir}/bin"
expect_rejected_without_staging_change missing-canonical "missing canonical Desktop binary"

: > "${source_dir}/bin/dagents-shell.exe"
expect_rejected_without_staging_change empty-canonical "missing canonical Desktop binary"

rm -f "${source_dir}/bin/dagents-shell.exe"
printf 'legacy alias\n' > "${source_dir}/bin/dagents-shell-tauri.exe"
expect_rejected_without_staging_change legacy-only "missing canonical Desktop binary"

printf 'canonical desktop\n' > "${source_dir}/bin/dagents-shell.exe"
expect_rejected_without_staging_change legacy-residual "retired tauri-suffixed Desktop alias is not accepted"

rm -f "${source_dir}/bin/dagents-shell-tauri.exe"
status=0
output="$(run_installer 2>&1)" || status=$?
[[ "${status}" -ne 0 ]] || fail "stub compiler unexpectedly produced an installer"
[[ "${output}" == *"expected output missing"* ]] || fail "canonical bundle did not pass payload validation"
[[ -s "${fixture}/bundle/bin/dagents-shell.exe" ]] || fail "valid preflight did not stage the canonical Desktop binary"
printf 'preserve installer staging\n' > "${fixture}/bundle/sentinel.txt"

mkdir -p "${fixture}/bundle/bin"
printf '0.11.0-test\n' > "${fixture}/bundle/VERSION"
printf 'canonical desktop\n' > "${fixture}/bundle/bin/dagents-shell.exe"
BUNDLE_SRC_OVERRIDE="${fixture}/bundle" expect_rejected_without_staging_change source-is-staging "bundle source must not be inside the staging directory"

mkdir -p "${fixture}/BUNDLE/bin"
printf '0.11.0-test\n' > "${fixture}/BUNDLE/VERSION"
printf 'canonical desktop\n' > "${fixture}/BUNDLE/bin/dagents-shell.exe"
BUNDLE_SRC_OVERRIDE="${fixture}/BUNDLE" \
  expect_rejected_without_staging_change case-aliased-source-is-staging "bundle source must not be inside the staging directory"
unset BUNDLE_SRC_OVERRIDE


echo "[test] Windows installer canonical Desktop payload preflight passed"
