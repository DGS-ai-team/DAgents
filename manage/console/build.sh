#!/usr/bin/env bash
# 构建 Manage Console（Vue）到 manage/console/static/
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "${ROOT}/manage/console/frontend"
if [[ ! -d node_modules ]]; then
  npm ci
fi
npm run build
echo "Built -> ${ROOT}/manage/console/static/"
