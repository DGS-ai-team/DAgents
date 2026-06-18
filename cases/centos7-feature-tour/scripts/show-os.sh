#!/usr/bin/env bash
# 打印容器 OS / glibc / 内核 / 静态二进制（需 docker compose up 后执行）
set -euo pipefail
docker compose exec dagents-node bash -c '
  echo "== /etc/redhat-release =="
  cat /etc/redhat-release
  echo "== glibc =="
  ldd --version 2>&1 | head -1
  echo "== kernel =="
  uname -r
  echo "== dagents-node =="
  file /usr/local/bin/dagents-node
  echo "== health =="
  curl -sf http://127.0.0.1:18765/health
  echo
'
