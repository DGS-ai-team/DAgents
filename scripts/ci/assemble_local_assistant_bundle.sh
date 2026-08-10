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

# Windows：同时打入 Tauri（推荐）与 Go legacy（兼容）两套 Shell；默认 dagents-shell.exe = Tauri。
if [[ "${PLATFORM}" == windows-* ]]; then
  SHELL_TAURI="${SHELL_TAURI_BIN:-${REPO_ROOT}/dist/dagents-shell-tauri${EXE}}"
  SHELL_LEGACY="${SHELL_LEGACY_BIN:-${REPO_ROOT}/dist/dagents-shell-legacy${EXE}}"
  SHELL_DEFAULT="${SHELL_BIN:-${REPO_ROOT}/dist/dagents-shell${EXE}}"
  if [[ "${SKIP_SHELL:-}" == "1" ]]; then
    echo "[assemble] SKIP_SHELL=1: omitting dagents-shell*"
  else
    if [[ ! -f "${SHELL_TAURI}" && -f "${SHELL_DEFAULT}" ]]; then
      SHELL_TAURI="${SHELL_DEFAULT}"
    fi
    if [[ ! -f "${SHELL_TAURI}" ]]; then
      echo "[assemble] missing dagents-shell-tauri${EXE} (run build_dagents_shell_tauri.sh)" >&2
      exit 1
    fi
    if [[ ! -f "${SHELL_LEGACY}" ]]; then
      echo "[assemble] missing dagents-shell-legacy${EXE} (run build_dagents_shell.sh)" >&2
      exit 1
    fi
    install -m 0755 "${SHELL_TAURI}" "${BUNDLE_DIR}/bin/dagents-shell-tauri${EXE}"
    install -m 0755 "${SHELL_LEGACY}" "${BUNDLE_DIR}/bin/dagents-shell-legacy${EXE}"
    install -m 0755 "${SHELL_TAURI}" "${BUNDLE_DIR}/bin/dagents-shell${EXE}"
  fi
elif [[ -f "${SHELL_BIN:-${REPO_ROOT}/dist/dagents-shell${EXE}}" ]]; then
  install -m 0755 "${SHELL_BIN:-${REPO_ROOT}/dist/dagents-shell${EXE}}" "${BUNDLE_DIR}/bin/dagents-shell${EXE}"
fi

if [[ -f "${REPO_ROOT}/packaging/agent-client/config.example.yaml" ]]; then
  cp "${REPO_ROOT}/packaging/agent-client/config.example.yaml" "${BUNDLE_DIR}/config.example.yaml"
fi
if [[ -f "${REPO_ROOT}/.env.example" ]]; then
  cp "${REPO_ROOT}/.env.example" "${BUNDLE_DIR}/.env.example"
fi
cp -a "${REPO_ROOT}/packaging/runtime/." "${BUNDLE_DIR}/.runtime/"

<<<<<<< HEAD
# 内置 Agent 模板（Node 亦 go:embed；打包保留磁盘副本便于覆盖/排查）
=======
# 内置 Agent 模板（磁盘副本；Node 亦可 embed）
>>>>>>> 0e49b9d (fix(linux): 对齐安装脚本与当前发布包现状)
if [[ -d "${REPO_ROOT}/packaging/agent-templates" ]]; then
  mkdir -p "${BUNDLE_DIR}/packaging"
  cp -a "${REPO_ROOT}/packaging/agent-templates" "${BUNDLE_DIR}/packaging/agent-templates"
fi

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

1. copy config.example.yaml to config.yaml (bootstrap: listen/local; configure LLM via Web UI)
2. Start Desktop Shell (recommended; tray supervises Node + HITL toasts):
     dagents shell --background          (logs in .runtime\logs\shell-YYYY-MM-DD.log)
     dagents shell status / shell stop
3. Or start Node only (legacy / debug):
     dagents                    (default: start Node in background)
     dagents node               (same: background + wait until ready)
     dagents node --foreground  (blocks terminal)
     dagents node --background  (background without wait)
     bin\dagents-node.exe -config config.yaml
     scripts\startup\windows\start-node.bat
   Logs: .runtime\logs\node-YYYY-MM-DD.log / .err.log
4. Open Web UI (embedded in dagents-node; no separate UI installer):
     http://127.0.0.1:<listen.port>/ui/   (default 18765; ui.enabled defaults to true)
     Printed after `dagents` or `dagents node` when the node is ready.
5. Browser tool (when browser.enabled: true in config):
     dagents browser --background          (recommended; logs .runtime\logs\browser-YYYY-MM-DD.log)
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
1. cp config.example.yaml config.yaml   # bootstrap：listen/local；LLM 等用 Web UI 配置
2. 启动 Node：
     ./dagents                    （默认：后台启动 Node 并打印 Web UI）
     ./dagents node               （同上：后台 + 等待就绪）
     ./dagents node --foreground  （前台阻塞）
     ./dagents node --background  （后台且不等待 probe）
     ./scripts/startup/linux/start-node.sh
   日志：.runtime/logs/node-YYYY-MM-DD.log / .err.log
3. 浏览器 Web UI（内嵌于 dagents-node，无需单独安装）：
     http://127.0.0.1:<listen.port>/ui/   （默认 18765；config 中 ui.enabled 默认 true）
     `dagents` 或 `dagents node` 就绪后会打印地址。
4. Browser 工具（config / Web UI 中启用 browser 能力时）：
     ./dagents browser --background    （推荐；日志 .runtime/logs/browser-YYYY-MM-DD.log）
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
