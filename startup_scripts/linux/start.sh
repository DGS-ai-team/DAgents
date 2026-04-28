#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${SCRIPT_DIR}"

if [[ ! -f ".env" ]]; then
  echo "[startup] .env 不存在，将使用 .env.example 与环境变量默认值。"
  echo "[startup] 如需自定义配置，请先复制：cp .env.example .env"
fi

if [[ ! -x "./dagents-api" ]]; then
  echo "[startup] 未找到可执行文件 ./dagents-api"
  exit 1
fi

export API_HOST="${API_HOST:-127.0.0.1}"
export API_PORT="${API_PORT:-8000}"

echo "[startup] starting dagents-api on ${API_HOST}:${API_PORT}"
exec ./dagents-api
