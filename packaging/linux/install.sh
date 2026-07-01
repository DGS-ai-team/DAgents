#!/usr/bin/env bash
# 将解压后的 DAgents Local Assistant 安装到固定目录，并配置 PATH / DAGENTS_HOME。
#
# 用法（在 tar.gz 解压目录内）：
#   ./install.sh                         # 用户级：~/.local/share/dagents + ~/.local/bin/dagents
#   sudo ./install.sh                    # 系统级：/opt/dagents + /usr/local/bin/dagents
#   ./install.sh --prefix ~/dagents      # 自定义目录
#   ./install.sh --uninstall             # 卸载（需与安装时相同的 --prefix）
#   ./install.sh --no-path               # 仅拷贝文件，不修改 shell 配置
set -euo pipefail

SOURCE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PREFIX=""
BIN_DIR=""
NO_PATH=0
UNINSTALL=0
OVERWRITE_POLICY=0
OVERWRITE_POLICY_SET=0
MARKER="# >>> dagents >>>"

usage() {
  cat <<'EOF'
用法:
  install.sh [--prefix DIR] [--bin-dir DIR] [--no-path]
             [--overwrite-policy | --keep-policy]
  install.sh --uninstall [--prefix DIR] [--bin-dir DIR]

默认:
  普通用户  PREFIX=~/.local/share/dagents   BIN_DIR=~/.local/bin
  root/sudo PREFIX=/opt/dagents            BIN_DIR=/usr/local/bin

说明:
  - 若 PREFIX 下已有 .runtime/node.pid，安装前先停止对应 Node（优先 dagents node shutdown）
  - 升级/重装：bin/、scripts/、dagents 启动脚本与配置示例始终更新；.runtime/ 默认仅补缺失路径
  - 若已有 .runtime/policy/，交互询问是否覆盖（--overwrite-policy / --keep-policy 可跳过询问）
  - 在 BIN_DIR 创建 dagents 符号链接
  - 写入 DAGENTS_HOME 与 PATH（含 `bin/`、`.runtime/externaltools/`；/etc/profile.d/dagents.sh 或 ~/.profile）
EOF
}

die() {
  echo "[install] error: $*" >&2
  exit 1
}

info() {
  echo "[install] $*"
}

NODE_STOP_TIMEOUT_SEC=15

pid_alive() {
  local pid="$1"
  [[ -n "${pid}" ]] && kill -0 "${pid}" 2>/dev/null
}

# 对指定 pid 先发 TERM，超时后 KILL（与 packaging/linux/dagents 一致）。
stop_pid_gracefully() {
  local pid="$1"
  if ! pid_alive "${pid}"; then
    return 0
  fi
  kill -TERM "${pid}" 2>/dev/null || true
  local waited=0
  while pid_alive "${pid}"; do
    sleep 1
    waited=$((waited + 1))
    if [[ "${waited}" -ge "${NODE_STOP_TIMEOUT_SEC}" ]]; then
      kill -KILL "${pid}" 2>/dev/null || true
      sleep 1
      break
    fi
  done
}

# 重装/升级同一 PREFIX 前：若已有 node.pid，先停掉旧 Node，避免覆盖二进制时进程仍占用 .runtime。
shutdown_existing_node_before_install() {
  local pid_file="${PREFIX}/.runtime/node.pid"
  if [[ ! -f "${pid_file}" ]]; then
    return 0
  fi

  info "found ${pid_file}, stopping existing node before install"
  # 优先走已安装 dagents（含 probe 与 pgrep 兜底），与日常 shutdown 行为一致。
  if [[ -x "${PREFIX}/dagents" ]]; then
    if "${PREFIX}/dagents" node shutdown; then
      return 0
    fi
    info "dagents node shutdown failed, falling back to pid kill"
  fi

  local pid
  pid="$(tr -d '[:space:]' <"${pid_file}")"
  if pid_alive "${pid}"; then
    info "stopping node process pid=${pid}"
    stop_pid_gracefully "${pid}"
  else
    info "removing stale node.pid (pid=${pid})"
  fi
  rm -f "${pid_file}"
}

default_paths() {
  if [[ -n "${PREFIX}" ]]; then
    return
  fi
  if [[ "$(id -u)" -eq 0 ]]; then
    PREFIX="/opt/dagents"
    BIN_DIR="${BIN_DIR:-/usr/local/bin}"
  else
    PREFIX="${HOME}/.local/share/dagents"
    BIN_DIR="${BIN_DIR:-${HOME}/.local/bin}"
  fi
}

validate_source() {
  local name
  for name in bin/dagents-node bin/dagents-client bin/dagents-cli dagents; do
    [[ -e "${SOURCE}/${name}" ]] || die "missing ${SOURCE}/${name}; run install.sh from extracted bundle root"
  done
}

copy_tree() {
  local src="$1" dst="$2"
  mkdir -p "${dst}"
  cp -a "${src}/." "${dst}/"
}

# 用户运行时数据目录：安装包不含内容，仅确保存在，不从 bundle 覆盖。
RUNTIME_USER_DATA_DIRS=(memory history logs agent)

ensure_runtime_user_dirs() {
  local d
  for d in "${RUNTIME_USER_DATA_DIRS[@]}"; do
    mkdir -p "${PREFIX}/.runtime/${d}"
  done
}

# .runtime 种子：默认仅拷贝目标尚不存在的路径（GNU cp -n）；可选覆盖 policy。
copy_runtime_seed() {
  local src="${SOURCE}/.runtime" dst="${PREFIX}/.runtime"
  ensure_runtime_user_dirs
  [[ -d "${src}" ]] || return 0
  mkdir -p "${dst}"
  cp -a -n "${src}/." "${dst}/"
  if [[ "${OVERWRITE_POLICY}" -eq 1 && -d "${src}/policy" ]]; then
    info "overwriting ${dst}/policy from bundle"
    mkdir -p "${dst}/policy"
    cp -a "${src}/policy/." "${dst}/policy/"
  fi
}

prompt_overwrite_policy() {
  [[ "${OVERWRITE_POLICY_SET}" -eq 1 ]] && return 0
  [[ -d "${SOURCE}/.runtime/policy" ]] || return 0
  [[ -f "${PREFIX}/.runtime/policy/tool.approval.txt" ]] || return 0
  if [[ ! -t 0 ]]; then
    info "existing policy kept (non-interactive; use --overwrite-policy to replace)"
    return 0
  fi
  local ans
  echo ""
  read -r -p "[install] 检测到已有 policy (${PREFIX}/.runtime/policy)，是否覆盖？[y/N] " ans
  case "${ans}" in
    y|Y|yes|YES)
      OVERWRITE_POLICY=1
      ;;
    *)
      OVERWRITE_POLICY=0
      ;;
  esac
}

install_files() {
  info "installing to ${PREFIX}"
  mkdir -p "${PREFIX}/bin" "${PREFIX}/.runtime" "${PREFIX}/scripts"
  copy_tree "${SOURCE}/bin" "${PREFIX}/bin"
  copy_runtime_seed
  copy_tree "${SOURCE}/scripts" "${PREFIX}/scripts"
  install -m 0755 "${SOURCE}/dagents" "${PREFIX}/dagents"
  if [[ -f "${SOURCE}/config.example.yaml" ]]; then
    install -m 0644 "${SOURCE}/config.example.yaml" "${PREFIX}/config.example.yaml"
  fi
  if [[ -f "${SOURCE}/.env.example" ]]; then
    install -m 0644 "${SOURCE}/.env.example" "${PREFIX}/.env.example"
  fi
  if [[ -f "${SOURCE}/README.txt" ]]; then
    install -m 0644 "${SOURCE}/README.txt" "${PREFIX}/README.txt"
  fi
  if [[ -f "${SOURCE}/VERSION" ]]; then
    install -m 0644 "${SOURCE}/VERSION" "${PREFIX}/VERSION"
  fi
  if [[ ! -f "${PREFIX}/config.yaml" && -f "${PREFIX}/config.example.yaml" ]]; then
    cp "${PREFIX}/config.example.yaml" "${PREFIX}/config.yaml"
    info "created ${PREFIX}/config.yaml from config.example.yaml"
  fi
  chmod +x "${PREFIX}/bin/"* "${PREFIX}/dagents" 2>/dev/null || true
  find "${PREFIX}/scripts" -type f -name '*.sh' -exec chmod +x {} + 2>/dev/null || true
}

write_env_file() {
  cat > "${PREFIX}/env.sh" <<EOF
# DAgents Local Assistant environment
export DAGENTS_HOME="${PREFIX}"
export PATH="${PREFIX}/bin:${PREFIX}/.runtime/externaltools:\${PATH}"
EOF
  chmod 0644 "${PREFIX}/env.sh"
}

link_launcher() {
  mkdir -p "${BIN_DIR}"
  ln -sfn "${PREFIX}/dagents" "${BIN_DIR}/dagents"
  info "linked ${BIN_DIR}/dagents -> ${PREFIX}/dagents"
}

setup_path_system() {
  local profile="/etc/profile.d/dagents.sh"
  cat > "${profile}" <<EOF
# DAgents Local Assistant
export DAGENTS_HOME="${PREFIX}"
export PATH="${PREFIX}/bin:${PREFIX}/.runtime/externaltools:\$PATH"
EOF
  chmod 0644 "${profile}"
  info "wrote ${profile}"
}

setup_path_user() {
  local rc added=0
  for rc in "${HOME}/.profile" "${HOME}/.bashrc"; do
    [[ -f "${rc}" ]] || continue
    if grep -qF "${MARKER}" "${rc}" 2>/dev/null; then
      info "shell config already present in ${rc}"
      added=1
      break
    fi
  done
  if [[ "${added}" -eq 0 ]]; then
    rc="${HOME}/.profile"
    [[ -f "${rc}" ]] || touch "${rc}"
    {
      echo ""
      echo "${MARKER}"
      echo ". \"${PREFIX}/env.sh\""
      echo "# <<< dagents <<<"
    } >> "${rc}"
    info "appended DAGENTS_HOME to ${rc}"
  fi
}

setup_path() {
  write_env_file
  link_launcher
  if [[ "$(id -u)" -eq 0 && "${PREFIX}" == /opt/* ]]; then
    setup_path_system
  else
    setup_path_user
  fi
  info "open a new shell or run: source \"${PREFIX}/env.sh\""
}

remove_path_system() {
  local profile="/etc/profile.d/dagents.sh"
  if [[ -f "${profile}" ]]; then
    rm -f "${profile}"
    info "removed ${profile}"
  fi
}

remove_path_user() {
  local rc
  for rc in "${HOME}/.profile" "${HOME}/.bashrc"; do
    [[ -f "${rc}" ]] || continue
    if grep -qF "${MARKER}" "${rc}" 2>/dev/null; then
      sed -i "/${MARKER}/,/# <<< dagents <<</d" "${rc}"
      info "removed dagents block from ${rc}"
    fi
  done
}

do_uninstall() {
  default_paths
  if [[ -L "${BIN_DIR}/dagents" || -f "${BIN_DIR}/dagents" ]]; then
    rm -f "${BIN_DIR}/dagents"
    info "removed ${BIN_DIR}/dagents"
  fi
  if [[ "$(id -u)" -eq 0 && "${PREFIX}" == /opt/* ]]; then
    remove_path_system
  else
    remove_path_user
  fi
  if [[ -d "${PREFIX}" ]]; then
    rm -rf "${PREFIX}"
    info "removed ${PREFIX}"
  else
    info "prefix not found: ${PREFIX}"
  fi
}

do_install() {
  default_paths
  prompt_overwrite_policy
  shutdown_existing_node_before_install
  validate_source
  install_files
  if [[ "${NO_PATH}" -eq 0 ]]; then
    setup_path
  else
    write_env_file
    link_launcher
  fi
  info "done. Try: dagents doctor"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --prefix)
      PREFIX="$2"
      shift 2
      ;;
    --bin-dir)
      BIN_DIR="$2"
      shift 2
      ;;
    --no-path)
      NO_PATH=1
      shift
      ;;
    --overwrite-policy)
      OVERWRITE_POLICY=1
      OVERWRITE_POLICY_SET=1
      shift
      ;;
    --keep-policy)
      OVERWRITE_POLICY=0
      OVERWRITE_POLICY_SET=1
      shift
      ;;
    --uninstall)
      UNINSTALL=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "unknown option: $1 (try --help)"
      ;;
  esac
done

if [[ "${UNINSTALL}" -eq 1 ]]; then
  do_uninstall
else
  do_install
fi
