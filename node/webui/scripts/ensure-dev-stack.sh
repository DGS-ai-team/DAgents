#!/usr/bin/env bash
# 供 VS Code/Cursor preLaunchTask 使用：已运行的 Node/Vite 直接复用，避免端口占用导致任务永不结束。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WS="$(cd "$SCRIPT_DIR/../../.." && pwd)"
NODE_URL="${DAGENTS_NODE_URL:-http://127.0.0.1:18765}"
VITE_PORT="${WEBUI_DEV_PORT:-5173}"
VITE_URL="http://127.0.0.1:${VITE_PORT}/ui/"

echo "dev stack starting"

node_healthy() {
  curl -sf "${NODE_URL}/health" >/dev/null 2>&1
}

vite_healthy() {
  curl -sf "$VITE_URL" >/dev/null 2>&1
}

if node_healthy; then
  echo "config loaded (reuse)"
  echo "agent node listening addr=${NODE_URL}"
else
  echo "config loaded"
  echo "starting dagents-node..."
  cd "$WS"
  exec go run ./node/cmd/dagents-node -config packaging/agent-client/config.yaml
fi
