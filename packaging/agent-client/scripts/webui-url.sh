#!/usr/bin/env bash
# 从 config.yaml 读取 listen.port，打印 Node 内嵌 Web UI 地址（默认 18765）。
set -euo pipefail

CFG="${1:-config.yaml}"
PORT="18765"

if [[ -f "${CFG}" ]]; then
  parsed="$(grep -A10 '^listen:' "${CFG}" 2>/dev/null | grep -E '^[[:space:]]+port:' | head -1 | sed -E 's/[^0-9]*([0-9]+).*/\1/' || true)"
  if [[ -n "${parsed}" ]] && [[ "${parsed}" =~ ^[0-9]+$ ]]; then
    PORT="${parsed}"
  fi
fi

echo "http://127.0.0.1:${PORT}/ui/"
