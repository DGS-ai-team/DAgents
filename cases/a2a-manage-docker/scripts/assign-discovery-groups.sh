#!/usr/bin/env bash
# 为案例 Node 分配 discovery_group（A2A discover / invoke 必需）
set -euo pipefail

MANAGE_URL="${MANAGE_URL:-http://127.0.0.1:8020}"
DISCOVERY_GROUP="${DISCOVERY_GROUP:-a2a-lab}"
AGENTS="${AGENTS:-node-a node-b}"

for agent_id in ${AGENTS}; do
  curl -sf -X PATCH "${MANAGE_URL}/v1/registry/agents/${agent_id}/groups" \
    -H "Content-Type: application/json" \
    -d "{\"discovery_group\":[\"${DISCOVERY_GROUP}\"]}"
  echo "assigned ${DISCOVERY_GROUP} -> ${agent_id}"
done
