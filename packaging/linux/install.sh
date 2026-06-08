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
MARKER="# >>> dagents >>>"

usage() {
  cat <<'EOF'
用法:
  install.sh [--prefix DIR] [--bin-dir DIR] [--no-path]
  install.sh --uninstall [--prefix DIR] [--bin-dir DIR]

默认:
  普通用户  PREFIX=~/.local/share/dagents   BIN_DIR=~/.local/bin
  root/sudo PREFIX=/opt/dagents            BIN_DIR=/usr/local/bin

说明:
  - 拷贝 bin/、.runtime/、scripts/、配置示例与 dagents 启动脚本
  - 在 BIN_DIR 创建 dagents 符号链接
  - 写入 DAGENTS_HOME 与 PATH（/etc/profile.d/dagents.sh 或 ~/.profile）
EOF
}

die() {
  echo "[install] error: $*" >&2
  exit 1
}

info() {
  echo "[install] $*"
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
  for name in bin/dagents-node bin/dagents-client bin/dagents-cli bin/dagents_register_center dagents; do
    [[ -e "${SOURCE}/${name}" ]] || die "missing ${SOURCE}/${name}; run install.sh from extracted bundle root"
  done
}

copy_tree() {
  local src="$1" dst="$2"
  mkdir -p "${dst}"
  cp -a "${src}/." "${dst}/"
}

install_files() {
  info "installing to ${PREFIX}"
  mkdir -p "${PREFIX}/bin" "${PREFIX}/.runtime" "${PREFIX}/scripts"
  copy_tree "${SOURCE}/bin" "${PREFIX}/bin"
  copy_tree "${SOURCE}/.runtime" "${PREFIX}/.runtime"
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
export PATH="${PREFIX}/bin:\${PATH}"
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
export PATH="${PREFIX}/bin:\$PATH"
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
