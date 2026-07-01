#!/usr/bin/env bash
# 本地启动 Manage + node-a + node-b（Docker 不可用时）
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPO="$(cd "${ROOT}/../.." && pwd)"
CASE="${ROOT}"
NODE_BIN="${DAGENTS_NODE:-/tmp/dagents-node}"
PID_DIR="${CASE}/local-run/pids"
LOG_DIR="${CASE}/local-run/logs"

mkdir -p "${PID_DIR}" "${LOG_DIR}" \
  "${CASE}/local-run/wd-a" "${CASE}/local-run/wd-b" \
  "${CASE}/runtime/node-a/prompt_context" "${CASE}/runtime/node-b/prompt_context"
cp -f "${CASE}/prompt_context/node-a/custom.md" "${CASE}/runtime/node-a/prompt_context/custom.md"
cp -f "${CASE}/prompt_context/node-b/custom.md" "${CASE}/runtime/node-b/prompt_context/custom.md"
cp -f "${CASE}/local-run/node-a.yaml" "${CASE}/local-run/wd-a/config.yaml"
cp -f "${CASE}/local-run/node-b.yaml" "${CASE}/local-run/wd-b/config.yaml"

if [ ! -x "${NODE_BIN}" ]; then
  (cd "${REPO}" && go build -o "${NODE_BIN}" ./node/cmd/dagents-node)
fi

stop_one() {
  local name="$1"
  local pidfile="${PID_DIR}/${name}.pid"
  if [ -f "${pidfile}" ]; then
    kill "$(cat "${pidfile}")" 2>/dev/null || true
    rm -f "${pidfile}"
  fi
}

stop_one manage
stop_one node-a
stop_one node-b

export MANAGE_HOST=127.0.0.1
export MANAGE_PORT=8020
export MANAGE_DB_PATH="${CASE}/local-run/manage.db"
export MANAGE_A2A_EXPIRE_SWEEP_SECONDS=10

(cd "${REPO}" && nohup python run_manage.py >"${LOG_DIR}/manage.log" 2>&1 & echo $! >"${PID_DIR}/manage.pid")
sleep 2

(cd "${CASE}/local-run/wd-a" && nohup "${NODE_BIN}" -config config.yaml >"${LOG_DIR}/node-a.log" 2>&1 & echo $! >"${PID_DIR}/node-a.pid")
(cd "${CASE}/local-run/wd-b" && nohup "${NODE_BIN}" -config config.yaml >"${LOG_DIR}/node-b.log" 2>&1 & echo $! >"${PID_DIR}/node-b.pid")
sleep 3

for id in node-a node-b; do
  curl -sf -X PATCH "http://127.0.0.1:8020/v1/registry/agents/${id}/groups" \
    -H "Content-Type: application/json" \
    -d '{"discovery_group":["a2a-lab"]}' >/dev/null || true
done

echo "Manage:  http://127.0.0.1:8020/console/"
echo "Node A:  http://127.0.0.1:18765  (合规助手)"
echo "Node B:  http://127.0.0.1:18766  (运维助手)"
echo "Logs:    ${LOG_DIR}/"
echo "Stop:    ${ROOT}/scripts/stop-local.sh"
