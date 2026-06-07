#!/usr/bin/env bash
# 启动 Go Agent Node（解压包根目录执行）。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT}"

CFG="${1:-config.yaml}"
if [[ ! -f "${CFG}" ]]; then
  echo "[startup] 未找到 ${CFG}，请先: cp config.example.yaml config.yaml"
  exit 1
fi
if [[ ! -x "./bin/dagents-node" ]]; then
  echo "[startup] 未找到 ./bin/dagents-node"
  exit 1
fi

echo "[startup] starting dagents-node -config ${CFG}"
exec ./bin/dagents-node -config "${CFG}"
