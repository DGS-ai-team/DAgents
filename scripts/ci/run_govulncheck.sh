#!/usr/bin/env bash
# Run the pinned Go vulnerability scanner over every Go module in the workspace.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${REPO_ROOT}"

GOVULNCHECK_VERSION="${GOVULNCHECK_VERSION:-v1.7.0}"
GOVULNCHECK_BIN="$(command -v govulncheck || true)"
TOOL_DIR=""
if [[ -z "${GOVULNCHECK_BIN}" ]]; then
  TOOL_DIR="$(mktemp -d)"
  trap 'rm -rf "${TOOL_DIR}"' EXIT
  GOBIN="${TOOL_DIR}" go install "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}"
  GOVULNCHECK_BIN="${TOOL_DIR}/govulncheck"
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
  echo "[govulncheck] ${module}"
  (cd "${module}" && "${GOVULNCHECK_BIN}" ./...)
done
