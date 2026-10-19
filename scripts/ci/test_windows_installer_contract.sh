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
cat > "${TEST_ROOT}/ISCC.exe" <<'STUB'
#!/bin/sh
set -eu
if [ "${STUB_CREATE_OUTPUT:-0}" = "1" ]; then
  output_base=""
  for arg in "$@"; do
    case "$arg" in
      //DMyOutputBaseFilename=*) output_base="${arg#*=}" ;;
    esac
  done
  [ -n "$output_base" ]
  mkdir -p "$STUB_OUTPUT_DIR"
  printf 'stub installer %s\n' "$output_base" > "$STUB_OUTPUT_DIR/${output_base}.exe"
fi
printf '%s\n' "$@" > "$STUB_ARGUMENTS"
exit 0
STUB
chmod +x "${TEST_ROOT}/ISCC.exe"
if command -v dash >/dev/null 2>&1; then
  STUB_CREATE_OUTPUT=1 \
  STUB_OUTPUT_DIR="${TEST_ROOT}/dash-smoke" \
  STUB_ARGUMENTS="${TEST_ROOT}/dash-smoke-arguments.txt" \
    dash "${TEST_ROOT}/ISCC.exe" '//DMyOutputBaseFilename=dash-smoke'
  test -s "${TEST_ROOT}/dash-smoke/dash-smoke.exe" || fail "stub compiler is not compatible with POSIX /bin/sh (dash)"
else
  printf '[test] SKIP: dash unavailable; POSIX /bin/sh stub syntax retained\n'
fi
printf '0.11.0\n' > "${source_dir}/VERSION"
printf '0.11.0\n' > "${fixture}/VERSION"
printf 'preserve installer staging\n' > "${fixture}/bundle/sentinel.txt"

run_installer() {
  local source="${BUNDLE_SRC_OVERRIDE:-${source_dir}}"
  PLATFORM=windows-amd64 \
  VERSION="${BUILD_VERSION:-0.11.0}" \
  INSTALLER_QA="${INSTALLER_QA:-0}" \
  QA_PACKAGE_VERSION="${QA_PACKAGE_VERSION:-}" \
  STUB_CREATE_OUTPUT="${STUB_CREATE_OUTPUT:-0}" \
  STUB_OUTPUT_DIR="${fixture}/dist-installer" \
  STUB_ARGUMENTS="${TEST_ROOT}/stub-arguments.txt" \
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
printf '0.11.0\n' > "${fixture}/bundle/VERSION"
printf 'canonical desktop\n' > "${fixture}/bundle/bin/dagents-shell.exe"
BUNDLE_SRC_OVERRIDE="${fixture}/bundle" expect_rejected_without_staging_change source-is-staging "bundle source must not be inside the staging directory"

mkdir -p "${fixture}/BUNDLE/bin"
printf '0.11.0\n' > "${fixture}/BUNDLE/VERSION"
printf 'canonical desktop\n' > "${fixture}/BUNDLE/bin/dagents-shell.exe"
BUNDLE_SRC_OVERRIDE="${fixture}/BUNDLE" \
  expect_rejected_without_staging_change case-aliased-source-is-staging "bundle source must not be inside the staging directory"
unset BUNDLE_SRC_OVERRIDE

BUILD_VERSION=0.11.1 expect_rejected_without_staging_change source-version-mismatch "VERSION mismatch"
QA_PACKAGE_VERSION=0.11.0.1 expect_rejected_without_staging_change production-qa-override "QA_PACKAGE_VERSION is allowed only with INSTALLER_QA=1"
INSTALLER_QA=1 QA_PACKAGE_VERSION=0.11.1.1 expect_rejected_without_staging_change qa-wrong-baseline "QA_PACKAGE_VERSION must append"
printf '0.11.2\n' > "${source_dir}/VERSION"
INSTALLER_QA=1 QA_PACKAGE_VERSION=0.11.0.1 expect_rejected_without_staging_change source-bundle-version-mismatch "VERSION mismatch"
printf '0.11.0\n' > "${source_dir}/VERSION"

for binary in dagents-node.exe dagents-client.exe dagents-browser.exe; do
  printf 'fixture %s\n' "$binary" > "${source_dir}/bin/$binary"
done
INSTALLER_QA=1 QA_PACKAGE_VERSION=0.11.0.1 STUB_CREATE_OUTPUT=1 run_installer >/dev/null
qa_installer="${fixture}/dist-installer/dagents-local-assistant-windows-amd64-installer-0.11.0.1-qa.exe"
[[ -s "${qa_installer}" ]] || fail "QA stub compiler did not create the expected synthetic package name"
grep -Fxq '//DMyAppVersion=0.11.0.1' "${TEST_ROOT}/stub-arguments.txt" || fail "QA compiler did not receive the synthetic package version"
grep -Fxq '//DIsolatedQaBuild=1' "${TEST_ROOT}/stub-arguments.txt" || fail "QA compiler did not receive the isolated QA macro"
"${PYTHON:-python3}" - "${qa_installer}.manifest.json" <<'PY'
import json, sys
with open(sys.argv[1], encoding="utf-8") as stream:
    manifest = json.load(stream)
assert manifest["version"] == "0.11.0.1"
assert manifest["packageVersion"] == "0.11.0.1"
assert manifest["packageVersionKind"] == "synthetic-qa-sequence"
assert manifest["bundleVersion"] == "0.11.0"
names = {item["name"] for item in manifest["payloads"]}
assert names == {"dagents-node.exe", "dagents-client.exe", "dagents-browser.exe", "dagents-shell.exe"}
PY
printf '[test] QA synthetic package version, compiler arguments, and manifest contract passed\n'


echo "[test] Windows installer canonical Desktop payload preflight passed"
