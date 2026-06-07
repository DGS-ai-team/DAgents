#!/usr/bin/env bash
# 启动 Register Center（可选 A2A 目录服务）。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT}"

if [[ ! -f ".env" && -f ".env.example" ]]; then
  echo "[startup] .env 不存在，将使用 .env.example 与环境变量默认值。"
  echo "[startup] 如需自定义: cp .env.example .env"
fi
if [[ ! -x "./bin/dagents_register_center" ]]; then
  echo "[startup] 未找到 ./bin/dagents_register_center"
  exit 1
fi

echo "[startup] starting dagents_register_center (REGISTER_CENTER_HOST/PORT 或 .env)"
exec ./bin/dagents_register_center
