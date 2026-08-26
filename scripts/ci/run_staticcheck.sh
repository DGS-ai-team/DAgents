#!/usr/bin/env bash
# Run the pinned Staticcheck release over every Go module in the workspace.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${REPO_ROOT}"

STATICCHECK_VERSION="v0.6.1"
STATICCHECK_BIN="$(command -v staticcheck || true)"
TOOL_DIR=""
if [[ -z "${STATICCHECK_BIN}" ]]; then
  TOOL_DIR="$(mktemp -d)"
  trap 'rm -rf "${TOOL_DIR}"' EXIT
  GOBIN="${TOOL_DIR}" go install "honnef.co/go/tools/cmd/staticcheck@${STATICCHECK_VERSION}"
  STATICCHECK_BIN="${TOOL_DIR}/staticcheck"
fi

MODULES=(
  shared/config
  shared/logfiles
  shared/update
  shared/workgroup
  node
  client
  desktop/tray
)

for module in "${MODULES[@]}"; do
  echo "[staticcheck] ${module}"
  (cd "${module}" && "${STATICCHECK_BIN}" ./...)
done
