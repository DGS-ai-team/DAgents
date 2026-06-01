#!/usr/bin/env bash
# RHEL 6 / SysV init：注册 dagents-node 为开机服务（非 systemd）。
#
# 用法（仓库根目录）：
#   sudo scripts/linux/install_node_service_sysv.sh install [--config PATH] [--binary PATH] [--build]
#   sudo scripts/linux/install_node_service_sysv.sh uninstall
#   scripts/linux/install_node_service_sysv.sh status
set -euo pipefail

SERVICE_NAME="dagents-node"
INIT_SCRIPT="/etc/init.d/${SERVICE_NAME}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

ACTION=""
CONFIG=""
BINARY=""
DO_BUILD=0

usage() {
  cat <<'EOF'
用法:
  install_node_service_sysv.sh install [--config PATH] [--binary PATH] [--build]
  install_node_service_sysv.sh uninstall
  install_node_service_sysv.sh status

说明:
  - 适用于 RHEL 6 / CentOS 6 等无 systemd 环境
  - install 需要 root
EOF
}

die() { echo "[error] $*" >&2; exit 1; }

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
  local c="${REPO_ROOT}/packaging/agent-client/config.yaml"
  [[ -f "${c}" ]] || c="${REPO_ROOT}/packaging/agent-client/config.example.yaml"
  [[ -f "${c}" ]] || die "config not found"
  abs_path "${c}"
}

resolve_binary() {
  if [[ -n "${BINARY}" ]]; then
    [[ -f "${BINARY}" ]] || die "binary not found: ${BINARY}"
    abs_path "${BINARY}"
    return
  fi
  local b="${REPO_ROOT}/dist/go-linux-amd64/bin/dagents-node"
  if [[ ! -f "${b}" ]]; then
    b="${REPO_ROOT}/bin/dagents-node"
  fi
  [[ -f "${b}" ]] || die "binary not found; use --build or --binary"
  abs_path "${b}"
}

install_service() {
  local cfg bin
  cfg="$(resolve_config)"
  if [[ "${DO_BUILD}" -eq 1 ]]; then
    bash "${REPO_ROOT}/scripts/ci/build_go_linux_static.sh"
  fi
  bin="$(resolve_binary)"
  [[ "$(id -u)" -eq 0 ]] || die "install requires root"

  cat > "${INIT_SCRIPT}" <<EOF
#!/bin/bash
# chkconfig: 35 99 01
# description: DAgents Agent Node (Go)

DAGENTS_CONFIG="${cfg}"
DAGENTS_BINARY="${bin}"
PIDFILE="/var/run/dagents-node.pid"
LOGFILE="/var/log/dagents-node.log"

case "\$1" in
  start)
    if [[ -f "\$PIDFILE" ]] && kill -0 "\$(cat "\$PIDFILE")" 2>/dev/null; then
      echo "already running"
      exit 0
    fi
    nohup "\$DAGENTS_BINARY" -config "\$DAGENTS_CONFIG" >>"\$LOGFILE" 2>&1 &
    echo \$! > "\$PIDFILE"
    echo "started pid=\$(cat "\$PIDFILE")"
    ;;
  stop)
    if [[ -f "\$PIDFILE" ]]; then
      kill "\$(cat "\$PIDFILE")" 2>/dev/null || true
      rm -f "\$PIDFILE"
    fi
    echo "stopped"
    ;;
  status)
    if [[ -f "\$PIDFILE" ]] && kill -0 "\$(cat "\$PIDFILE")" 2>/dev/null; then
      echo "running pid=\$(cat "\$PIDFILE")"
      exit 0
    fi
    echo "not running"
    exit 1
    ;;
  restart)
    \$0 stop
    sleep 1
    \$0 start
    ;;
  *)
    echo "Usage: \$0 {start|stop|status|restart}"
    exit 2
    ;;
esac
EOF
  chmod 755 "${INIT_SCRIPT}"
  if command -v chkconfig >/dev/null 2>&1; then
    chkconfig --add "${SERVICE_NAME}" || true
    chkconfig "${SERVICE_NAME}" on || true
  fi
  echo "[ok] installed ${INIT_SCRIPT}"
  echo "     config=${cfg}"
  echo "     binary=${bin}"
}

uninstall_service() {
  [[ "$(id -u)" -eq 0 ]] || die "uninstall requires root"
  if [[ -x "${INIT_SCRIPT}" ]]; then
    "${INIT_SCRIPT}" stop || true
  fi
  if command -v chkconfig >/dev/null 2>&1; then
    chkconfig --del "${SERVICE_NAME}" 2>/dev/null || true
  fi
  rm -f "${INIT_SCRIPT}"
  echo "[ok] removed ${INIT_SCRIPT}"
}

status_service() {
  if [[ -x "${INIT_SCRIPT}" ]]; then
    "${INIT_SCRIPT}" status
  else
    echo "not installed (${INIT_SCRIPT} missing)"
    exit 1
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    install|uninstall|status) ACTION="$1"; shift ;;
    --config) CONFIG="$2"; shift 2 ;;
    --binary) BINARY="$2"; shift 2 ;;
    --build) DO_BUILD=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown arg: $1" ;;
  esac
done

[[ -n "${ACTION}" ]] || { usage; exit 2; }

case "${ACTION}" in
  install) install_service ;;
  uninstall) uninstall_service ;;
  status) status_service ;;
esac
