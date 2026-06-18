#!/usr/bin/env bash
# 将已编译的 dagents-node + dagents-client + dagents-cli
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
VERSION="${VERSION:-0.3.8}"

EXE=""
if [[ "${PLATFORM}" == windows-* ]]; then
  EXE=".exe"
fi

NODE_BIN="${NODE_BIN:-${REPO_ROOT}/dist/dagents-node${EXE}}"
CLIENT_BIN="${CLIENT_BIN:-${REPO_ROOT}/dist/dagents-client${EXE}}"
CLI_BIN="${CLI_BIN:-${REPO_ROOT}/dist/dagents-cli${EXE}}"

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

BUNDLE_NAME="dagents-local-assistant-${PLATFORM}"
BUNDLE_DIR="${REPO_ROOT}/dist/${BUNDLE_NAME}"
ARCHIVE_BASE="${REPO_ROOT}/dist/${BUNDLE_NAME}-${VERSION}"

rm -rf "${BUNDLE_DIR}"
mkdir -p "${BUNDLE_DIR}/bin" "${BUNDLE_DIR}/.runtime"

install -m 0755 "${NODE_BIN}" "${BUNDLE_DIR}/bin/dagents-node${EXE}"
install -m 0755 "${CLIENT_BIN}" "${BUNDLE_DIR}/bin/dagents-client${EXE}"
install -m 0755 "${CLI_BIN}" "${BUNDLE_DIR}/bin/dagents-cli${EXE}"

if [[ -f "${REPO_ROOT}/packaging/agent-client/config.example.yaml" ]]; then
  cp "${REPO_ROOT}/packaging/agent-client/config.example.yaml" "${BUNDLE_DIR}/config.example.yaml"
fi
if [[ -f "${REPO_ROOT}/packaging/agent-client/agent-card.example.json" ]]; then
  cp "${REPO_ROOT}/packaging/agent-client/agent-card.example.json" "${BUNDLE_DIR}/agent-card.example.json"
fi
if [[ -f "${REPO_ROOT}/packaging/agent-client/agent-card.example.ops.json" ]]; then
  cp "${REPO_ROOT}/packaging/agent-client/agent-card.example.ops.json" "${BUNDLE_DIR}/agent-card.example.ops.json"
fi
if [[ -f "${REPO_ROOT}/.env.example" ]]; then
  cp "${REPO_ROOT}/.env.example" "${BUNDLE_DIR}/.env.example"
fi
cp -a "${REPO_ROOT}/packaging/runtime/." "${BUNDLE_DIR}/.runtime/"

# 启动脚本与 Node 系统服务注册脚本
mkdir -p "${BUNDLE_DIR}/scripts"
cp -a "${REPO_ROOT}/packaging/agent-client/scripts/startup" "${BUNDLE_DIR}/scripts/"
if [[ -f "${REPO_ROOT}/packaging/agent-client/scripts/webui-url.sh" ]]; then
  install -m 0755 "${REPO_ROOT}/packaging/agent-client/scripts/webui-url.sh" "${BUNDLE_DIR}/scripts/webui-url.sh"
fi
if [[ -f "${REPO_ROOT}/packaging/agent-client/scripts/webui-url.bat" ]]; then
  install -m 0644 "${REPO_ROOT}/packaging/agent-client/scripts/webui-url.bat" "${BUNDLE_DIR}/scripts/webui-url.bat"
fi
cp -a "${REPO_ROOT}/scripts/linux" "${BUNDLE_DIR}/scripts/"
cp -a "${REPO_ROOT}/scripts/windows" "${BUNDLE_DIR}/scripts/"
cp -a "${REPO_ROOT}/scripts/service" "${BUNDLE_DIR}/scripts/"
if [[ -f "${REPO_ROOT}/packaging/agent-client/scripts/README.md" ]]; then
  cp "${REPO_ROOT}/packaging/agent-client/scripts/README.md" "${BUNDLE_DIR}/scripts/README.md"
fi
find "${BUNDLE_DIR}/scripts" -type f -name '*.sh' -exec chmod 0755 {} +

echo "${VERSION}" > "${BUNDLE_DIR}/VERSION"

if [[ "${PLATFORM}" == linux-* ]]; then
  install -m 0755 "${REPO_ROOT}/packaging/linux/dagents" "${BUNDLE_DIR}/dagents"
  install -m 0755 "${REPO_ROOT}/packaging/linux/install.sh" "${BUNDLE_DIR}/install.sh"
fi

if [[ "${PLATFORM}" == windows-* ]]; then
  install -m 0644 "${REPO_ROOT}/packaging/windows/dagents.cmd" "${BUNDLE_DIR}/dagents.cmd"
fi

if [[ "${PLATFORM}" == windows-* ]]; then
  cat > "${BUNDLE_DIR}/README.txt" <<'EOF'
DAgents Local Assistant (Go Node + dual TUI)

1. copy config.example.yaml to config.yaml and edit llm / agent_id
   copy agent-card.example.json agent-card.json          (Manage/A2A callee)
   copy agent-card.example.ops.json agent-card.json      (ops caller only)
2. Start Node:
     dagents node --background          (recommended; logs in .runtime\logs\node.log)
     dagents node                       (foreground)
     bin\dagents-node.exe -config config.yaml
     scripts\startup\windows\start-node.bat
3. Browser Web UI (embedded in dagents-node; no separate UI installer):
     http://127.0.0.1:<listen.port>/ui/   (default 18765; ui.enabled defaults to true)

Install Node as SYSTEM startup task (admin CMD):
  scripts\windows\install_node_service.cmd install config.yaml

TUI（pick one; --withnode auto-starts Node if not running):
3a. dagents chat --withnode
    Python Textual TUI (rich UI, recommended on modern terminals)
3b. dagents tui --withnode
    Go bubbletea full-screen TUI (default; child agents, /children, etc.)
3c. dagents tui --withnode --plain
    Go line-mode REPL (legacy SSH / dumb terminal)

Optional third-party CLIs (not bundled; see .runtime/RECOMMENDED_CLI_TOOLS.md):
  OfficeCLI for .docx/.xlsx/.pptx — install then /skill load officecli

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

便携使用：
1. cp config.example.yaml config.yaml && 编辑 llm / agent_id
   cp agent-card.example.json agent-card.json            # Manage/A2A 被调方
   cp agent-card.example.ops.json agent-card.json        # 纯调用方（ops）
2. 启动 Node：
     ./dagents node --background    （推荐；日志 .runtime/logs/node.log）
     ./dagents node                 （前台）
     ./scripts/startup/linux/start-node.sh
3. 浏览器 Web UI（内嵌于 dagents-node，无需单独安装）：
     http://127.0.0.1:<listen.port>/ui/   （默认 18765；config 中 ui.enabled 默认 true）

安装到固定目录（推荐）：
  ./install.sh              用户级 ~/.local/share/dagents
  sudo ./install.sh         系统级 /opt/dagents（写入 /etc/profile.d/dagents.sh）
  安装后新开 shell，执行 dagents doctor 验证

A2A / Registry 控制面请单独部署 Manage（见 packaging/manage/README.md）。

注册 Node 为 systemd 服务（需 root）：
  sudo ./scripts/linux/install_node_service.sh install --config config.yaml

TUI（三选一；--withnode 会在 Node 未运行时自动后台启动）：
  ./dagents chat --withnode         Python Textual TUI（现代终端，富 UI）
  ./dagents tui --withnode          Go bubbletea 全屏 TUI（含子 Agent、/children 等）
  ./dagents tui --withnode --plain  Go 行模式 REPL（老 SSH / dumb 终端）

Web UI 与 TUI 可并存；dagents-node 二进制已通过 go:embed 打包前端静态资源。

可选第三方 CLI（发布包不含；见 .runtime/RECOMMENDED_CLI_TOOLS.md）：
  OfficeCLI（Office 文档）— 自行安装后 /skill load officecli

文档: docs/architecture/local-assistant.md
EOF
  ARCHIVE="${ARCHIVE_BASE}.tar.gz"
  rm -f "${ARCHIVE}"
  tar -C "${REPO_ROOT}/dist" -czf "${ARCHIVE}" "$(basename "${BUNDLE_DIR}")"
fi

echo "[done] ${ARCHIVE}"
