#!/usr/bin/env bash
# Build the reusable Vue frontend into node/webui/frontend/dist/.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "${ROOT}/node/webui/frontend"
if [[ ! -d node_modules ]]; then
  if [[ -f package-lock.json ]]; then
    npm ci
  else
    npm install
  fi
fi
npm run build
echo "Built reusable frontend -> ${ROOT}/node/webui/frontend/dist/"
