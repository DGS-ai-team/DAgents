#!/usr/bin/env bash
# 将已编译的 dagents-node + dagents-client + dagents-cli + dagents_register_center
# 与配置、.runtime、scripts/ 组装为发布目录并压缩。
#
# 用法：
#   PLATFORM=linux-amd64 VERSION=0.2.2 scripts/ci/assemble_local_assistant_bundle.sh
#   PLATFORM=windows-amd64 VERSION=0.2.2 scripts/ci/assemble_local_assistant_bundle.sh
#
# 前置：dist/dagents-node[.exe]、dist/dagents-cli[.exe] 已存在（或由 NODE_BIN/CLI_BIN 指定）。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

PLATFORM="${PLATFORM:-linux-amd64}"
VERSION="${VERSION:-0.2.2}"

EXE=""
if [[ "${PLATFORM}" == windows-* ]]; then
  EXE=".exe"
fi

NODE_BIN="${NODE_BIN:-${REPO_ROOT}/dist/dagents-node${EXE}}"
CLIENT_BIN="${CLIENT_BIN:-${REPO_ROOT}/dist/dagents-client${EXE}}"
CLI_BIN="${CLI_BIN:-${REPO_ROOT}/dist/dagents-cli${EXE}}"
RC_BIN="${RC_BIN:-${REPO_ROOT}/dist/dagents_register_center${EXE}}"

if [[ ! -f "${NODE_BIN}" ]]; then
  echo "[assemble] missing node binary: ${NODE_BIN}" >&2
  exit 1
fi
if [[ ! -f "${CLIENT_BIN}" ]]; then
  echo "[assemble] missing client binary: ${CLIENT_BIN}" >&2
  exit 1
fi
if [[ ! -f "${CLI_BIN}" ]]; then
  echo "[assemble] missing cli binary: ${CLI_BIN}" >&2
  exit 1
fi
if [[ ! -f "${RC_BIN}" ]]; then
  echo "[assemble] missing register center binary: ${RC_BIN}" >&2
  exit 1
fi

BUNDLE_NAME="dagents-local-assistant-${PLATFORM}"
BUNDLE_DIR="${REPO_ROOT}/dist/${BUNDLE_NAME}"
ARCHIVE_BASE="${REPO_ROOT}/dist/${BUNDLE_NAME}-${VERSION}"

rm -rf "${BUNDLE_DIR}"
mkdir -p "${BUNDLE_DIR}/bin" "${BUNDLE_DIR}/.runtime"

install -m 0755 "${NODE_BIN}" "${BUNDLE_DIR}/bin/dagents-node${EXE}"
install -m 0755 "${CLIENT_BIN}" "${BUNDLE_DIR}/bin/dagents-client${EXE}"
install -m 0755 "${CLI_BIN}" "${BUNDLE_DIR}/bin/dagents-cli${EXE}"
install -m 0755 "${RC_BIN}" "${BUNDLE_DIR}/bin/dagents_register_center${EXE}"

if [[ -f "${REPO_ROOT}/packaging/agent-client/config.example.yaml" ]]; then
  cp "${REPO_ROOT}/packaging/agent-client/config.example.yaml" "${BUNDLE_DIR}/config.example.yaml"
fi
if [[ -f "${REPO_ROOT}/.env.example" ]]; then
  cp "${REPO_ROOT}/.env.example" "${BUNDLE_DIR}/.env.example"
fi
cp -a "${REPO_ROOT}/packaging/runtime/." "${BUNDLE_DIR}/.runtime/"

# 启动脚本与 Node 系统服务注册脚本
mkdir -p "${BUNDLE_DIR}/scripts"
cp -a "${REPO_ROOT}/packaging/agent-client/scripts/startup" "${BUNDLE_DIR}/scripts/"
cp -a "${REPO_ROOT}/scripts/linux" "${BUNDLE_DIR}/scripts/"
cp -a "${REPO_ROOT}/scripts/windows" "${BUNDLE_DIR}/scripts/"
cp -a "${REPO_ROOT}/scripts/service" "${BUNDLE_DIR}/scripts/"
if [[ -f "${REPO_ROOT}/packaging/agent-client/scripts/README.md" ]]; then
  cp "${REPO_ROOT}/packaging/agent-client/scripts/README.md" "${BUNDLE_DIR}/scripts/README.md"
fi
find "${BUNDLE_DIR}/scripts" -type f -name '*.sh' -exec chmod 0755 {} +

if [[ "${PLATFORM}" == windows-* ]]; then
  cat > "${BUNDLE_DIR}/README.txt" <<'EOF'
DAgents Local Assistant (Go Node + dual TUI)

1. copy config.example.yaml to config.yaml and edit llm / agent_id
2. bin\dagents-node.exe -config config.yaml
   or scripts\startup\windows\start-node.bat

Register Center (optional A2A):
  copy .env.example .env
  scripts\startup\windows\start-register-center.bat

Install Node as SYSTEM startup task (admin CMD):
  scripts\windows\install_node_service.cmd install config.yaml

TUI (pick one):
3a. bin\dagents-cli.exe chat --config config.yaml
    Python Textual TUI (rich UI, recommended on modern terminals)
3b. bin\dagents-client.exe -config config.yaml tui
    Go bubbletea full-screen TUI (default; child agents, /children, etc.)
3c. bin\dagents-client.exe -config config.yaml tui --plain
    Go line-mode REPL (legacy SSH / dumb terminal)

See docs/architecture/local-assistant.md
EOF
  ARCHIVE="${ARCHIVE_BASE}.zip"
  rm -f "${ARCHIVE}"
  if command -v zip >/dev/null 2>&1; then
    (cd "${REPO_ROOT}/dist" && zip -rq "$(basename "${ARCHIVE}")" "$(basename "${BUNDLE_DIR}")")
  else
    ARCHIVE="${ARCHIVE_BASE}.tar.gz"
    tar -C "${REPO_ROOT}/dist" -czf "${ARCHIVE}" "$(basename "${BUNDLE_DIR}")"
  fi
else
  cat > "${BUNDLE_DIR}/README.txt" <<'EOF'
DAgents Local Assistant（Go Node + 双 TUI）

1. cp config.example.yaml config.yaml && 编辑 llm / agent_id
2. ./bin/dagents-node -config config.yaml
   或 ./scripts/startup/linux/start-node.sh

Register Center（可选 A2A）：
  cp .env.example .env
  ./scripts/startup/linux/start-register-center.sh

注册 Node 为 systemd 服务（需 root）：
  sudo ./scripts/linux/install_node_service.sh install --config config.yaml

TUI（三选一）：
3a. ./bin/dagents-cli chat --config config.yaml
    Python Textual TUI（现代终端，富 UI）
3b. ./bin/dagents-client -config config.yaml tui
    Go bubbletea 全屏 TUI（默认；含子 Agent、/children 等）
3c. ./bin/dagents-client -config config.yaml tui --plain
    Go 行模式 REPL（老 SSH / dumb 终端）

文档: docs/architecture/local-assistant.md
EOF
  ARCHIVE="${ARCHIVE_BASE}.tar.gz"
  rm -f "${ARCHIVE}"
  tar -C "${REPO_ROOT}/dist" -czf "${ARCHIVE}" "$(basename "${BUNDLE_DIR}")"
fi

echo "[done] ${ARCHIVE}"
