#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${SCRIPT_DIR}"

if [[ ! -f ".env" ]]; then
  echo "[startup] .env 不存在，将使用 .env.example 与环境变量默认值。"
  echo "[startup] 如需自定义配置，请先复制：cp .env.example .env"
fi

if [[ ! -x "./dagents_register_center" ]]; then
  echo "[startup] 未找到可执行文件 ./dagents_register_center"
  exit 1
fi

echo "[startup] starting dagents_register_center (host/port 由 .env 或程序默认值决定)"
exec ./dagents_register_center
