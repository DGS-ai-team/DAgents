#!/usr/bin/env bash
# Run the repository's complete local quality gate.
#
# Usage: scripts/verify.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

GO_PACKAGES=(
  ./shared/config/...
  ./shared/logfiles/...
  ./shared/update/...
  ./shared/workgroup/...
  ./node/...
  ./client/...
  ./desktop/tray/...
)

echo "[verify] install and build Node Web UI"
npm ci --prefix node/webui/frontend
npm run build --prefix node/webui/frontend
npm test --prefix node/webui/frontend
npm run lint --prefix node/webui/frontend

echo "[verify] build Manage Console"
npm ci --prefix manage/console/frontend
npm run build --prefix manage/console/frontend
npm run lint --prefix manage/console/frontend

echo "[verify] Python quality"
python3 -m pip install --requirement requirements.lock --requirement requirements-dev.txt
python3 -m ruff check manage scripts tests
python3 -m pyright --project pyrightconfig.json
python3 -m unittest discover -s tests -p "test_*.py" -v
python3 scripts/ci/check_contracts.py
python3 scripts/ci/sync_openapi_routes.py --check

echo "[verify] Go formatting, vet, tests, and build"
mapfile -t GO_FILES < <(git ls-files '*.go')
test -z "$(gofmt -l "${GO_FILES[@]}")"
go vet "${GO_PACKAGES[@]}"
echo "[verify] Staticcheck"
bash scripts/ci/run_staticcheck.sh
go test "${GO_PACKAGES[@]}"
go build -o "${TMPDIR:-/tmp}/dagents-node-verify" ./node/cmd/dagents-node
go build -o "${TMPDIR:-/tmp}/dagents-client-verify" ./client/cmd/dagents-client

if command -v cargo >/dev/null 2>&1; then
  echo "[verify] Rust formatting and tests"
  cargo fmt --check --manifest-path desktop/tray-tauri/src-tauri/Cargo.toml
  cargo test --locked --manifest-path desktop/tray-tauri/src-tauri/Cargo.toml
fi

echo "[verify] repository hygiene"
git diff --check
echo "[verify] passed"
