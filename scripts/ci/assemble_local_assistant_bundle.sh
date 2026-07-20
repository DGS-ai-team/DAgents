#!/usr/bin/env bash
# 将已编译的 dagents-node + dagents-client + dagents-browser
# 与配置、.runtime、scripts/ 组装为发布目录并压缩。
#
# 用法：
#   PLATFORM=linux-amd64 VERSION=0.2.2 scripts/ci/assemble_local_assistant_bundle.sh
#   PLATFORM=windows-amd64 VERSION=0.2.2 scripts/ci/assemble_local_assistant_bundle.sh
#   SKIP_BROWSER=1 ...   # 跳过 dagents-browser（本地调试无 PyInstaller 产物时）
#
# 前置：dist/dagents-node[.exe]、dist/dagents-client[.exe]；dist/dagents-browser[.exe] 可选（或 SKIP_BROWSER=1）。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

PLATFORM="${PLATFORM:-linux-amd64}"
VERSION="${VERSION:-0.4.0}"

EXE=""
if [[ "${PLATFORM}" == windows-* ]]; then
  EXE=".exe"
fi

NODE_BIN="${NODE_BIN:-${REPO_ROOT}/dist/dagents-node${EXE}}"
CLIENT_BIN="${CLIENT_BIN:-${REPO_ROOT}/dist/dagents-client${EXE}}"
BROWSER_BIN="${BROWSER_BIN:-${REPO_ROOT}/dist/dagents-browser${EXE}}"

if [[ ! -f "${NODE_BIN}" ]]; then
  echo "[assemble] missing node binary: ${NODE_BIN}" >&2
  exit 1
fi
if [[ ! -f "${CLIENT_BIN}" ]]; then
  echo "[assemble] missing client binary: ${CLIENT_BIN}" >&2
  exit 1
fi
if [[ ! -f "${BROWSER_BIN}" ]]; then
  if [[ "${SKIP_BROWSER:-}" == "1" ]]; then
    echo "[assemble] SKIP_BROWSER=1: omitting dagents-browser"
  else
    echo "[assemble] missing browser binary: ${BROWSER_BIN} (run build_dagents_browser.sh or set SKIP_BROWSER=1)" >&2
    exit 1
  fi
fi

BUNDLE_NAME="dagents-local-assistant-${PLATFORM}"
BUNDLE_DIR="${REPO_ROOT}/dist/${BUNDLE_NAME}"
ARCHIVE_BASE="${REPO_ROOT}/dist/${BUNDLE_NAME}-${VERSION}"

rm -rf "${BUNDLE_DIR}"
mkdir -p "${BUNDLE_DIR}/bin" "${BUNDLE_DIR}/.runtime"

install -m 0755 "${NODE_BIN}" "${BUNDLE_DIR}/bin/dagents-node${EXE}"
install -m 0755 "${CLIENT_BIN}" "${BUNDLE_DIR}/bin/dagents-client${EXE}"
if [[ -f "${BROWSER_BIN}" ]]; then
  install -m 0755 "${BROWSER_BIN}" "${BUNDLE_DIR}/bin/dagents-browser${EXE}"
fi

SHELL_BIN="${SHELL_BIN:-${REPO_ROOT}/dist/dagents-shell${EXE}}"
if [[ -f "${SHELL_BIN}" ]]; then
  install -m 0755 "${SHELL_BIN}" "${BUNDLE_DIR}/bin/dagents-shell${EXE}"
elif [[ "${PLATFORM}" == windows-* && "${SKIP_SHELL:-}" != "1" ]]; then
  echo "[assemble] missing dagents-shell${EXE} (run build_dagents_shell.sh or set SKIP_SHELL=1)" >&2
  exit 1
fi

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
  install -m 0644 "${REPO_ROOT}/packaging/windows/write-install-config.ps1" "${BUNDLE_DIR}/scripts/windows/write-install-config.ps1"
fi

if [[ "${PLATFORM}" == windows-* ]]; then
  cat > "${BUNDLE_DIR}/README.txt" <<'EOF'
DAgents Local Assistant (Go Node + Desktop Shell + Web UI)

1. copy config.example.yaml to config.yaml and edit llm / agent_id / agent.role (Manage A2A)
2. Start Desktop Shell (recommended; tray supervises Node + HITL toasts):
     dagents shell --background          (logs in .runtime\logs\shell.log)
     dagents shell status / shell stop
3. Or start Node only (legacy / debug):
     dagents                    (default: start Node in background)
     dagents node --background  (logs in .runtime\logs\node.log)
     dagents node               (foreground)
     bin\dagents-node.exe -config config.yaml
     scripts\startup\windows\start-node.bat
4. Open Web UI (embedded in dagents-node; no separate UI installer):
     http://127.0.0.1:<listen.port>/ui/   (default 18765; ui.enabled defaults to true)
     Printed after `dagents` or `dagents node` when the node is ready.
5. Browser tool (when browser.enabled: true in config):
     dagents browser --background          (recommended; logs .runtime\logs\browser.log)
     dagents browser stop
     bin\dagents-browser.exe --config config.yaml

Install Node as SYSTEM startup task (admin CMD):
  scripts\windows\install_node_service.cmd install config.yaml

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
DAgents Local Assistant（Go Node + Web UI）

便携使用：
1. cp config.example.yaml config.yaml && 编辑 llm / agent_id / agent.role（Manage A2A）
2. 启动 Node：
     ./dagents                    （默认：后台启动 Node）
     ./dagents node --background  （推荐；日志 .runtime/logs/node.log）
     ./dagents node               （前台）
     ./scripts/startup/linux/start-node.sh
3. 浏览器 Web UI（内嵌于 dagents-node，无需单独安装）：
     http://127.0.0.1:<listen.port>/ui/   （默认 18765；config 中 ui.enabled 默认 true）
     `dagents` 或 `dagents node` 就绪后会打印地址。
4. Browser 工具（config 中 browser.enabled: true 时）：
     ./dagents browser --background    （推荐；日志 .runtime/logs/browser.log）
     ./dagents browser stop
     ./bin/dagents-browser --config config.yaml

安装到固定目录（推荐）：
  ./install.sh              用户级 ~/.local/share/dagents
  sudo ./install.sh         系统级 /opt/dagents（写入 /etc/profile.d/dagents.sh）
  安装后新开 shell，执行 dagents doctor 验证

A2A / Registry 控制面请单独部署 Manage（见 packaging/manage/README.md）。

注册 Node 为 systemd 服务（需 root）：
  sudo ./scripts/linux/install_node_service.sh install --config config.yaml

dagents-node 二进制已通过 go:embed 打包前端静态资源；人机界面请使用 Web UI。

可选第三方 CLI（发布包不含；见 .runtime/RECOMMENDED_CLI_TOOLS.md）：
  OfficeCLI（Office 文档）— 自行安装后 /skill load officecli

文档: docs/architecture/local-assistant.md
EOF
  ARCHIVE="${ARCHIVE_BASE}.tar.gz"
  rm -f "${ARCHIVE}"
  tar -C "${REPO_ROOT}/dist" -czf "${ARCHIVE}" "$(basename "${BUNDLE_DIR}")"
fi

echo "[done] ${ARCHIVE}"
