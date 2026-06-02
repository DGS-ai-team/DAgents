#!/usr/bin/env bash
# 将 dagents-node 注册为 systemd 服务（dagents-node.service）。
#
# 用法（仓库根目录）：
#   sudo scripts/linux/install_node_service.sh install [--config PATH] [--binary PATH] [--build] [--env-file PATH]
#   sudo scripts/linux/install_node_service.sh uninstall
#   scripts/linux/install_node_service.sh status
set -euo pipefail

SERVICE_UNIT="dagents-node.service"
SYSTEMD_PATH="/etc/systemd/system/${SERVICE_UNIT}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
TEMPLATE="${SCRIPT_DIR}/../service/dagents-node.service.template"

ACTION=""
CONFIG=""
BINARY=""
WORKING_DIR=""
ENV_FILE=""
DO_BUILD=0

usage() {
  cat <<'EOF'
用法:
  install_node_service.sh install [--config PATH] [--binary PATH] [--working-dir PATH] [--build] [--env-file PATH]
  install_node_service.sh uninstall
  install_node_service.sh status

说明:
  - install 需要 root（sudo）
  - --config 默认: DAGENTS_CONFIG 或 packaging/agent-client/config.yaml
  - --binary 默认: bin/dagents-node 或 PATH 中的 dagents-node
EOF
}

die() {
  echo "[error] $*" >&2
  exit 1
}

abs_path() {
  local p="$1"
  if command -v realpath >/dev/null 2>&1; then
    realpath "$p"
  else
    echo "$(cd "$(dirname "$p")" && pwd)/$(basename "$p")"
  fi
}

resolve_config() {
  if [[ -n "${CONFIG}" ]]; then
    [[ -f "${CONFIG}" ]] || die "config not found: ${CONFIG}"
    abs_path "${CONFIG}"
    return
  fi
  if [[ -n "${DAGENTS_CONFIG:-}" ]]; then
    [[ -f "${DAGENTS_CONFIG}" ]] || die "DAGENTS_CONFIG not found: ${DAGENTS_CONFIG}"
    abs_path "${DAGENTS_CONFIG}"
    return
  fi
  local candidates=(
    "${REPO_ROOT}/config.yaml"
    "${REPO_ROOT}/config.example.yaml"
    "${REPO_ROOT}/packaging/agent-client/config.yaml"
    "${REPO_ROOT}/packaging/agent-client/config.example.yaml"
  )
  local c
  for c in "${candidates[@]}"; do
    if [[ -f "${c}" ]]; then
      abs_path "${c}"
      return
    fi
  done
  die "config not found: pass --config or set DAGENTS_CONFIG"
}

resolve_binary() {
  if [[ -n "${BINARY}" ]]; then
    [[ -f "${BINARY}" ]] || die "binary not found: ${BINARY}"
    abs_path "${BINARY}"
    return
  fi
  if command -v dagents-node >/dev/null 2>&1; then
    command -v dagents-node
    return
  fi
  local candidates=(
    "${REPO_ROOT}/bin/dagents-node"
    "${REPO_ROOT}/dagents-node"
  )
  local c
  for c in "${candidates[@]}"; do
    if [[ -f "${c}" ]]; then
      abs_path "${c}"
      return
    fi
  done
  die "dagents-node not found: pass --binary or use --build"
}

build_binary() {
  mkdir -p "${REPO_ROOT}/bin"
  echo "[install] go build -o bin/dagents-node ./node/cmd/dagents-node"
  (cd "${REPO_ROOT}" && go build -o bin/dagents-node ./node/cmd/dagents-node)
  abs_path "${REPO_ROOT}/bin/dagents-node"
}

render_unit() {
  local binary="$1" config="$2" workdir="$3" env_file_line=""
  [[ -f "${TEMPLATE}" ]] || die "missing template: ${TEMPLATE}"
  if [[ -n "${ENV_FILE}" ]]; then
    env_file_line="EnvironmentFile=-${ENV_FILE}"
  fi
  sed \
    -e "s|@BINARY@|${binary}|g" \
    -e "s|@CONFIG@|${config}|g" \
    -e "s|@WORKING_DIR@|${workdir}|g" \
    -e "s|@ENV_FILE_LINE@|${env_file_line}|g" \
    "${TEMPLATE}"
}

cmd_install() {
  if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
    die "install 需要 root: sudo $0 install ..."
  fi
  local config binary workdir
  config="$(resolve_config)"
  if [[ "${DO_BUILD}" -eq 1 ]]; then
    binary="$(build_binary)"
  else
    binary="$(resolve_binary)"
  fi
  if [[ -n "${WORKING_DIR}" ]]; then
    workdir="$(abs_path "${WORKING_DIR}")"
  else
    workdir="${REPO_ROOT}"
  fi
  render_unit "${binary}" "${config}" "${workdir}" >"${SYSTEMD_PATH}"
  echo "[install] wrote ${SYSTEMD_PATH}"
  systemctl daemon-reload
  systemctl enable --now "${SERVICE_UNIT}"
  echo "[install] enabled and started ${SERVICE_UNIT}"
  systemctl status "${SERVICE_UNIT}" --no-pager || true
}

cmd_uninstall() {
  if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
    die "uninstall 需要 root"
  fi
  systemctl disable --now "${SERVICE_UNIT}" 2>/dev/null || true
  if [[ -f "${SYSTEMD_PATH}" ]]; then
    rm -f "${SYSTEMD_PATH}"
    echo "[uninstall] removed ${SYSTEMD_PATH}"
  else
    echo "[uninstall] unit not present: ${SYSTEMD_PATH}"
  fi
  systemctl daemon-reload
}

cmd_status() {
  command -v systemctl >/dev/null 2>&1 || die "systemctl not found"
  systemctl is-enabled "${SERVICE_UNIT}" 2>/dev/null || true
  systemctl is-active "${SERVICE_UNIT}" 2>/dev/null || true
  systemctl status "${SERVICE_UNIT}" --no-pager 2>/dev/null || true
}

parse_install_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --config)
        CONFIG="${2:-}"
        shift 2
        ;;
      --binary)
        BINARY="${2:-}"
        shift 2
        ;;
      --working-dir)
        WORKING_DIR="${2:-}"
        shift 2
        ;;
      --env-file)
        ENV_FILE="${2:-}"
        shift 2
        ;;
      --build)
        DO_BUILD=1
        shift
        ;;
      *)
        die "unknown option: $1"
        ;;
    esac
  done
}

main() {
  ACTION="${1:-}"
  shift || true
  case "${ACTION}" in
    install)
      parse_install_args "$@"
      cmd_install
      ;;
    uninstall)
      cmd_uninstall
      ;;
    status)
      cmd_status
      ;;
    -h|--help|help|"")
      usage
      exit 0
      ;;
    *)
      usage
      die "unknown action: ${ACTION}"
      ;;
  esac
}

main "$@"
