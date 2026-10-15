#!/usr/bin/env bash
# 将已编译的 dagents-node + dagents-client + dagents-browser
# 与配置、.runtime、scripts/ 组装为发布目录；Linux 再生成 tar.gz 归档。
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
VERSION="${VERSION:-}"

if [[ -z "${VERSION}" ]]; then
  echo "[assemble] VERSION is required; pass the release/package version explicitly" >&2
  exit 1
fi
case "${PLATFORM}" in
  linux-amd64|windows-amd64|windows-386)
    ;;
  *)
    echo "[assemble] unsupported platform: ${PLATFORM}" >&2
    exit 1
    ;;
esac

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

# Resolve every binary before changing the previous staging directory. Inputs
# may point into a prior staging tree, so deleting first would destroy them.
SHELL_TAURI=""
if [[ "${PLATFORM}" == windows-* && "${SKIP_SHELL:-}" != "1" ]]; then
  SHELL_TAURI="${SHELL_TAURI_BIN:-${REPO_ROOT}/dist/dagents-shell-tauri${EXE}}"
  SHELL_DEFAULT="${SHELL_BIN:-${REPO_ROOT}/dist/dagents-shell${EXE}}"
  if [[ ! -f "${SHELL_TAURI}" && -f "${SHELL_DEFAULT}" ]]; then
    SHELL_TAURI="${SHELL_DEFAULT}"
  fi
  if [[ ! -f "${SHELL_TAURI}" ]]; then
    echo "[assemble] missing dagents-shell-tauri${EXE} (run build_dagents_shell_tauri.sh)" >&2
    exit 1
  fi
fi

BUNDLE_NAME="dagents-local-assistant-${PLATFORM}"
BUNDLE_DIR="${REPO_ROOT}/dist/${BUNDLE_NAME}"
ARCHIVE_BASE="${REPO_ROOT}/dist/${BUNDLE_NAME}-${VERSION}"
ARCHIVE=""

repo_root_real="$(cd "${REPO_ROOT}" && pwd -P)"
dist_path="${REPO_ROOT}/dist"
if [[ -L "${dist_path}" ]]; then
  echo "[assemble] refusing a symlinked dist directory: ${dist_path}" >&2
  exit 1
fi
mkdir -p "${dist_path}"
dist_real="$(cd "${dist_path}" && pwd -P)"
bundle_parent_real="$(cd "$(dirname "${BUNDLE_DIR}")" && pwd -P)"
expected_bundle_name="dagents-local-assistant-${PLATFORM}"
if [[ "${dist_real}" != "${repo_root_real}/dist" ||
      "${bundle_parent_real}" != "${dist_real}" ||
      "$(basename "${BUNDLE_DIR}")" != "${expected_bundle_name}" ||
      "${BUNDLE_DIR}" != "${REPO_ROOT}/dist/${expected_bundle_name}" ||
      -L "${BUNDLE_DIR}" ]]; then
  echo "[assemble] refusing to replace an unexpected bundle path: ${BUNDLE_DIR}" >&2
  exit 1
fi
bundle_real="${dist_real}/${expected_bundle_name}"
assert_input_outside_bundle() {
  local label="$1"
  local input="$2"
  local input_real
  local compare_input compare_bundle
  if [[ -L "${input}" ]]; then
    echo "[assemble] refusing symlinked ${label} input: ${input}" >&2
    exit 1
  fi
  input_real="$(cd "$(dirname "${input}")" && pwd -P)/$(basename "${input}")"
  compare_input="${input_real}"
  compare_bundle="${bundle_real}"
  if [[ "${PLATFORM}" == windows-* ]]; then
    compare_input="${compare_input,,}"
    compare_bundle="${compare_bundle,,}"
  fi
  case "${compare_input}" in
    "${compare_bundle}"|"${compare_bundle}/"*)
      echo "[assemble] ${label} input must not be inside the bundle directory being replaced: ${input}" >&2
      exit 1
      ;;
  esac
}

assert_input_outside_bundle "Node" "${NODE_BIN}"
assert_input_outside_bundle "Client" "${CLIENT_BIN}"
if [[ -f "${BROWSER_BIN}" ]]; then
  assert_input_outside_bundle "Browser" "${BROWSER_BIN}"
fi
if [[ -n "${SHELL_TAURI}" ]]; then
  assert_input_outside_bundle "Desktop Shell" "${SHELL_TAURI}"
fi

rm -rf "${BUNDLE_DIR}"
mkdir -p "${BUNDLE_DIR}/bin" "${BUNDLE_DIR}/.runtime"

install -m 0755 "${NODE_BIN}" "${BUNDLE_DIR}/bin/dagents-node${EXE}"
install -m 0755 "${CLIENT_BIN}" "${BUNDLE_DIR}/bin/dagents-client${EXE}"
if [[ -f "${BROWSER_BIN}" ]]; then
  install -m 0755 "${BROWSER_BIN}" "${BUNDLE_DIR}/bin/dagents-browser${EXE}"
fi

# Windows: bundle only the standalone Tauri Desktop Shell; Go legacy is not distributed.
if [[ "${PLATFORM}" == windows-* ]]; then
  SHELL_TAURI="${SHELL_TAURI_BIN:-${REPO_ROOT}/dist/dagents-shell-tauri${EXE}}"
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
    install -m 0755 "${SHELL_TAURI}" "${BUNDLE_DIR}/bin/dagents-shell-tauri${EXE}"
    install -m 0755 "${SHELL_TAURI}" "${BUNDLE_DIR}/bin/dagents-shell${EXE}"
  fi
fi

if [[ -f "${REPO_ROOT}/packaging/agent-client/config.example.yaml" ]]; then
  cp "${REPO_ROOT}/packaging/agent-client/config.example.yaml" "${BUNDLE_DIR}/config.example.yaml"
fi
if [[ -f "${REPO_ROOT}/.env.example" ]]; then
  cp "${REPO_ROOT}/.env.example" "${BUNDLE_DIR}/.env.example"
fi
cp -a "${REPO_ROOT}/packaging/runtime/." "${BUNDLE_DIR}/.runtime/"

# 内置 Agent 模板（Node 亦 go:embed；打包保留磁盘副本便于覆盖/排查）
if [[ -d "${REPO_ROOT}/packaging/agent-templates" ]]; then
  mkdir -p "${BUNDLE_DIR}/packaging"
  cp -a "${REPO_ROOT}/packaging/agent-templates" "${BUNDLE_DIR}/packaging/agent-templates"
fi

# 启动脚本与 Node 系统服务注册脚本
mkdir -p "${BUNDLE_DIR}/scripts"
cp -a "${REPO_ROOT}/packaging/agent-client/scripts/startup" "${BUNDLE_DIR}/scripts/"
if [[ -f "${REPO_ROOT}/packaging/agent-client/scripts/node-api-url.sh" ]]; then
  install -m 0755 "${REPO_ROOT}/packaging/agent-client/scripts/node-api-url.sh" "${BUNDLE_DIR}/scripts/node-api-url.sh"
fi
if [[ -f "${REPO_ROOT}/packaging/agent-client/scripts/node-api-url.bat" ]]; then
  install -m 0644 "${REPO_ROOT}/packaging/agent-client/scripts/node-api-url.bat" "${BUNDLE_DIR}/scripts/node-api-url.bat"
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
DAgents Local Assistant (Go Node + Desktop Shell)

1. copy config.example.yaml to config.yaml (Node bootstrap: listen only)
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
4. Connect Desktop to the local Node API (no UI is served by Node):
     http://127.0.0.1:<listen.port>   (default 18765)
     The Node startup command prints this API endpoint when ready.
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
  echo "[assemble] Windows portable archive disabled; staging directory is for Inno Setup only"
else
  cat > "${BUNDLE_DIR}/README.txt" <<'EOF'
DAgents Local Assistant（Go Node API）

便携使用：
1. cp config.example.yaml config.yaml   # Node bootstrap：仅 listen
2. 启动 Node：
     ./dagents                    （默认：后台启动 Node 并打印 API endpoint）
     ./dagents node               （同上：后台 + 等待就绪）
     ./dagents node --foreground  （前台阻塞）
     ./dagents node --background  （后台且不等待 probe）
     ./scripts/startup/linux/start-node.sh
   日志：.runtime/logs/node-YYYY-MM-DD.log / .err.log
3. DAgents Desktop 通过 Node API 连接本机 Node：
     http://127.0.0.1:<listen.port>   （默认 18765）
     Node 就绪后 `dagents` 或 `dagents node` 会打印 API endpoint。
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

DAgents Desktop 提供独立本地界面；Node 只提供 API。

可选第三方 CLI（发布包不含；见 .runtime/RECOMMENDED_CLI_TOOLS.md）：
  OfficeCLI（Office 文档）— 自行安装后 /skill load officecli

文档: docs/architecture/local-assistant.md
EOF
  ARCHIVE="${ARCHIVE_BASE}.tar.gz"
  rm -f "${ARCHIVE}"
  tar -C "${REPO_ROOT}/dist" -czf "${ARCHIVE}" "$(basename "${BUNDLE_DIR}")"
fi

echo "[done] ${BUNDLE_DIR}"
if [[ -n "${ARCHIVE}" ]]; then
  echo "[done] ${ARCHIVE}"
fi
