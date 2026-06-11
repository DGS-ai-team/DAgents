#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PID_DIR="${ROOT}/local-run/pids"
for name in node-b node-a manage; do
  pidfile="${PID_DIR}/${name}.pid"
  if [ -f "${pidfile}" ]; then
    kill "$(cat "${pidfile}")" 2>/dev/null || true
    rm -f "${pidfile}"
    echo "stopped ${name}"
  fi
done
