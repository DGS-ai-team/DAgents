#!/usr/bin/env bash
# 将已编译的 dagents-node + dagents-cli 与配置、.runtime 组装为发布目录并压缩。
#
# 用法：
#   PLATFORM=linux-amd64 VERSION=0.2.0 scripts/ci/assemble_local_assistant_bundle.sh
#   PLATFORM=windows-amd64 VERSION=0.2.0 scripts/ci/assemble_local_assistant_bundle.sh
#
# 前置：dist/dagents-node[.exe]、dist/dagents-cli[.exe] 已存在（或由 NODE_BIN/CLI_BIN 指定）。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

PLATFORM="${PLATFORM:-linux-amd64}"
VERSION="${VERSION:-0.2.0}"

EXE=""
if [[ "${PLATFORM}" == windows-* ]]; then
  EXE=".exe"
fi

NODE_BIN="${NODE_BIN:-${REPO_ROOT}/dist/dagents-node${EXE}}"
CLI_BIN="${CLI_BIN:-${REPO_ROOT}/dist/dagents-cli${EXE}}"

if [[ ! -f "${NODE_BIN}" ]]; then
  echo "[assemble] missing node binary: ${NODE_BIN}" >&2
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
install -m 0755 "${CLI_BIN}" "${BUNDLE_DIR}/bin/dagents-cli${EXE}"

if [[ -f "${REPO_ROOT}/packaging/agent-client/config.example.yaml" ]]; then
  cp "${REPO_ROOT}/packaging/agent-client/config.example.yaml" "${BUNDLE_DIR}/config.example.yaml"
fi
cp -a "${REPO_ROOT}/packaging/runtime/." "${BUNDLE_DIR}/.runtime/"

if [[ "${PLATFORM}" == windows-* ]]; then
  cat > "${BUNDLE_DIR}/README.txt" <<'EOF'
DAgents Local Assistant (Go Node + Textual TUI)

1. copy config.example.yaml to config.yaml and edit llm / agent_id
2. bin\dagents-node.exe -config config.yaml
3. bin\dagents-cli.exe chat --config config.yaml

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
DAgents Local Assistant（Go Node + Textual TUI）

1. cp config.example.yaml config.yaml && 编辑 llm / agent_id
2. ./bin/dagents-node -config config.yaml
3. ./bin/dagents-cli chat --config config.yaml

文档: docs/architecture/local-assistant.md
EOF
  ARCHIVE="${ARCHIVE_BASE}.tar.gz"
  rm -f "${ARCHIVE}"
  tar -C "${REPO_ROOT}/dist" -czf "${ARCHIVE}" "$(basename "${BUNDLE_DIR}")"
fi

echo "[done] ${ARCHIVE}"
