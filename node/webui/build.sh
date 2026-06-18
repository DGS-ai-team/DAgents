#!/usr/bin/env bash
# 构建 Node Web UI（Vue）到 node/internal/webui/static/
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
echo "Built -> ${ROOT}/node/internal/webui/static/"
