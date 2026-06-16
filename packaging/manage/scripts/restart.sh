#!/usr/bin/env bash
# 启动或重启 Manage 容器（需先 import-image.sh）。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"

die() {
  echo "[restart] error: $*" >&2
  exit 1
}

info() {
  echo "[restart] $*"
}

if ! command -v docker >/dev/null 2>&1; then
  die "docker not found"
fi

if [[ ! -f "${ROOT}/.env" ]]; then
  if [[ -f "${ROOT}/.env.example" ]]; then
    cp "${ROOT}/.env.example" "${ROOT}/.env"
    info "created .env from .env.example"
  else
    die "missing .env and .env.example"
  fi
fi

COMPOSE_FILE="${ROOT}/docker-compose.yml"
[[ -f "${COMPOSE_FILE}" ]] || die "missing ${COMPOSE_FILE}"

if docker compose version >/dev/null 2>&1; then
  COMPOSE=(docker compose -f "${COMPOSE_FILE}")
elif command -v docker-compose >/dev/null 2>&1; then
  COMPOSE=(docker-compose -f "${COMPOSE_FILE}")
else
  die "docker compose / docker-compose not found"
fi

if "${COMPOSE[@]}" ps -q manage 2>/dev/null | grep -q .; then
  info "restarting manage"
  "${COMPOSE[@]}" restart manage
else
  info "starting manage"
  "${COMPOSE[@]}" up -d
fi

info "health: curl -sf http://127.0.0.1:${MANAGE_PORT:-8020}/health"
info "console: http://127.0.0.1:${MANAGE_PORT:-8020}/console/"
