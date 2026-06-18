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
if [[ -x "${ROOT}/scripts/webui-url.sh" ]]; then
  WEBUI_URL="$("${ROOT}/scripts/webui-url.sh" "${CFG}")"
else
  PORT="18765"
  if [[ -f "${CFG}" ]]; then
    parsed="$(grep -A10 '^listen:' "${CFG}" 2>/dev/null | grep -E '^[[:space:]]+port:' | head -1 | sed -E 's/[^0-9]*([0-9]+).*/\1/' || true)"
    [[ -n "${parsed}" ]] && PORT="${parsed}"
  fi
  WEBUI_URL="http://127.0.0.1:${PORT}/ui/"
fi
echo "[startup] Web UI: ${WEBUI_URL} (内嵌于 dagents-node；ui.enabled 默认 true)"
exec ./bin/dagents-node -config "${CFG}"
