#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd -P)"
ASSEMBLER="${REPO_ROOT}/scripts/ci/assemble_local_assistant_bundle.sh"
WORKSPACE_REAL="$(cd "${REPO_ROOT}" && pwd -P)"
TEST_ROOT="$(mktemp -d "${REPO_ROOT}/.runtime-test-assembler-contract.XXXXXX")"

cleanup() {
  case "${TEST_ROOT}" in
    "${WORKSPACE_REAL}"/.runtime-test-assembler-contract.*)
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

new_fixture() {
  local name="$1"
  local root="${TEST_ROOT}/${name}"
  mkdir -p \
    "${root}/scripts/ci" \
    "${root}/scripts/linux" \
    "${root}/scripts/windows" \
    "${root}/scripts/service" \
    "${root}/packaging/agent-client/scripts/startup" \
    "${root}/packaging/runtime" \
    "${root}/packaging/windows" \
    "${root}/dist"
  cp "${ASSEMBLER}" "${root}/scripts/ci/assemble_local_assistant_bundle.sh"
  : > "${root}/packaging/windows/dagents.cmd"
  : > "${root}/packaging/windows/write-install-config.ps1"
  printf 'node fixture\n' > "${root}/dist/node.exe"
  printf 'client fixture\n' > "${root}/dist/client.exe"
  printf 'browser fixture\n' > "${root}/dist/browser.exe"
  printf 'shell fixture\n' > "${root}/dist/shell.exe"
  printf '%s\n' "${root}"
}

prepare_old_staging() {
  local root="$1"
  local bundle="${root}/dist/dagents-local-assistant-windows-amd64"
  mkdir -p "${bundle}/bin"
  printf 'keep this staging tree\n' > "${bundle}/sentinel.txt"
  printf 'old client\n' > "${bundle}/bin/dagents-client.exe"
  printf 'old browser\n' > "${bundle}/bin/dagents-browser.exe"
  printf 'old shell\n' > "${bundle}/bin/dagents-shell-tauri.exe"
}

run_assembler() {
  local root="$1"
  shift
  env \
    PLATFORM=windows-amd64 \
    VERSION=0.11.0-test \
    NODE_BIN="${root}/dist/node.exe" \
    CLIENT_BIN="${root}/dist/client.exe" \
    BROWSER_BIN="${root}/dist/browser.exe" \
    SHELL_TAURI_BIN="${root}/dist/shell.exe" \
    "$@" \
    bash "${root}/scripts/ci/assemble_local_assistant_bundle.sh"
}

expect_preserved_failure() {
  local case_name="$1"
  local source_var="$2"
  local root bundle source
  root="$(new_fixture "${case_name}")"
  prepare_old_staging "${root}"
  bundle="${root}/dist/dagents-local-assistant-windows-amd64"
  case "${source_var}" in
    CLIENT_BIN) source="${bundle}/bin/dagents-client.exe" ;;
    BROWSER_BIN) source="${bundle}/bin/dagents-browser.exe" ;;
    SHELL_TAURI_BIN) source="${bundle}/bin/dagents-shell-tauri.exe" ;;
    *) fail "unknown fixture input: ${source_var}" ;;
  esac
  if run_assembler "${root}" "${source_var}=${source}" > /dev/null 2>&1; then
    fail "${case_name}: assembler unexpectedly accepted an input from the replaced staging tree"
  fi
  [[ -f "${bundle}/sentinel.txt" ]] || fail "${case_name}: old staging sentinel was deleted"
  [[ "$(cat "${bundle}/sentinel.txt")" == "keep this staging tree" ]] || fail "${case_name}: old staging sentinel changed"
}

expect_preserved_failure client-from-staging CLIENT_BIN
expect_preserved_failure browser-from-staging BROWSER_BIN
expect_preserved_failure shell-from-staging SHELL_TAURI_BIN

root="$(new_fixture windows-case-alias)"
prepare_old_staging "${root}"
bundle="${root}/dist/dagents-local-assistant-windows-amd64"
alias="${root}/dist/DAgents-local-assistant-windows-amd64"
mkdir -p "${alias}/bin"
printf 'case alias client\n' > "${alias}/bin/dagents-client.exe"
if run_assembler "${root}" CLIENT_BIN="${alias}/bin/dagents-client.exe" > /dev/null 2>&1; then
  fail "windows-case-alias: assembler accepted a Windows case-alias input"
fi
[[ -f "${bundle}/sentinel.txt" ]] || fail "windows-case-alias: old staging sentinel was deleted"

root="$(new_fixture missing-shell)"
prepare_old_staging "${root}"
bundle="${root}/dist/dagents-local-assistant-windows-amd64"
if run_assembler "${root}" \
  SHELL_TAURI_BIN="${root}/dist/missing-shell.exe" \
  SHELL_BIN="${root}/dist/missing-default-shell.exe" > /dev/null 2>&1; then
  fail "missing-shell: assembler unexpectedly accepted a missing required shell"
fi
[[ -f "${bundle}/sentinel.txt" ]] || fail "missing-shell: old staging sentinel was deleted"

root="$(new_fixture skip-optional-payloads)"
run_assembler "${root}" \
  SKIP_BROWSER=1 \
  BROWSER_BIN="${root}/dist/missing-browser.exe" \
  SKIP_SHELL=1 \
  SHELL_TAURI_BIN="${root}/dist/missing-shell.exe" > /dev/null
bundle="${root}/dist/dagents-local-assistant-windows-amd64"
[[ -f "${bundle}/bin/dagents-node.exe" ]] || fail "skip-optional-payloads: Node was omitted"
[[ ! -e "${bundle}/bin/dagents-browser.exe" ]] || fail "skip-optional-payloads: skipped browser was packaged"
[[ ! -e "${bundle}/bin/dagents-shell.exe" ]] || fail "skip-optional-payloads: skipped shell was packaged"

root="$(new_fixture successful-assembly)"
cp -a "${REPO_ROOT}/packaging/runtime/." "${root}/packaging/runtime/"
run_assembler "${root}" > /dev/null
bundle="${root}/dist/dagents-local-assistant-windows-amd64"
for expected in \
  "${bundle}/bin/dagents-node.exe" \
  "${bundle}/bin/dagents-client.exe" \
  "${bundle}/bin/dagents-browser.exe" \
  "${bundle}/bin/dagents-shell.exe" \
  "${bundle}/bin/dagents-shell-tauri.exe"; do
  [[ -f "${expected}" ]] || fail "successful-assembly: missing expected payload $(basename "${expected}")"
done
[[ ! -e "${bundle}/sentinel.txt" ]] || fail "successful-assembly: stale staging sentinel survived replacement"
skill_source="${REPO_ROOT}/packaging/runtime/skills/write-skill/SKILL.md"
skill_staged="${bundle}/.runtime/skills/write-skill/SKILL.md"
[[ -s "${skill_staged}" ]] || fail "successful-assembly: real write-skill body was not packaged"
cmp -s "${skill_source}" "${skill_staged}" || fail "successful-assembly: packaged write-skill body differs from source"
if find "${bundle}/bin" -type f \( -iname '*tray*' -o -iname '*.so' \) -print -quit | grep -q .; then
  fail "successful-assembly: retired Go tray or plugin binary found in staged bin"
fi
retired_guard_status=0
grep -RniE 'protect-loaded-skill|loaded_skill_file_guard|builtin\.loaded_skill_file_guard' "${bundle}/.runtime" > /dev/null || retired_guard_status=$?
if [[ "${retired_guard_status}" -eq 0 ]]; then
  fail "successful-assembly: retired protect-loaded-skill payload found"
fi
[[ "${retired_guard_status}" -eq 1 ]] || fail "successful-assembly: failed to inspect staged runtime for retired guard files"

echo "[test] assemble bundle input-preservation and Windows payload contracts passed"
