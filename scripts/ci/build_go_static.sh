#!/usr/bin/env bash
# 交叉编译 Go Agent Node + Client（CGO_ENABLED=0）。
#
# 用法（仓库根目录）：
#   scripts/ci/build_go_static.sh
#   OUT_DIR=dist/pkg GOOS=windows GOARCH=amd64 scripts/ci/build_go_static.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

OUT_DIR="${OUT_DIR:-${REPO_ROOT}/dist/go-linux-amd64}"
GOOS="${GOOS:-linux}"
GOARCH="${GOARCH:-amd64}"
LDFLAGS="${LDFLAGS:--s -w}"

EXE=""
if [[ "${GOOS}" == "windows" ]]; then
	EXE=".exe"
fi

mkdir -p "${OUT_DIR}/bin"

build_one() {
  local pkg="$1"
  local out="$2"
  echo "[build] ${GOOS}/${GOARCH} ${pkg} -> ${out}"
  (
    cd "${REPO_ROOT}"
    CGO_ENABLED=0 GOOS="${GOOS}" GOARCH="${GOARCH}" \
      go build -ldflags="${LDFLAGS}" -o "${out}" "${pkg}"
  )
}

BUILD_CLIENT="${BUILD_CLIENT:-0}"

build_one "./node/cmd/dagents-node" "${OUT_DIR}/bin/dagents-node${EXE}"
if [[ "${BUILD_CLIENT}" == "1" ]]; then
  build_one "./client/cmd/dagents-client" "${OUT_DIR}/bin/dagents-client${EXE}"
fi

if [[ -f "${REPO_ROOT}/packaging/agent-client/config.example.yaml" ]]; then
  cp "${REPO_ROOT}/packaging/agent-client/config.example.yaml" "${OUT_DIR}/config.example.yaml"
fi

if [[ "${BUILD_CLIENT}" == "1" ]]; then
  if [[ "${GOOS}" == "windows" ]]; then
    cat > "${OUT_DIR}/README.txt" <<'EOF'
DAgents Agent Node + Client (Go static build, Windows)

1. copy config.example.yaml to config.yaml and edit llm / agent_id
2. bin\dagents-node.exe -config config.yaml
3. bin\dagents-client.exe -config config.yaml tui
   legacy terminal: add --plain

See docs/architecture/go-node-compatibility.md
EOF
  else
    cat > "${OUT_DIR}/README.txt" <<'EOF'
DAgents Agent Node + Client (Go static build)

1. cp config.example.yaml config.yaml && 编辑 llm / agent_id
2. ./bin/dagents-node -config config.yaml
3. ./bin/dagents-client -config config.yaml tui
   老终端: ./bin/dagents-client -config config.yaml tui --plain

文档: docs/architecture/go-node-compatibility.md
EOF
  fi
else
  if [[ "${GOOS}" == "windows" ]]; then
    cat > "${OUT_DIR}/README.txt" <<'EOF'
DAgents Agent Node (Go static build, Windows)

Use scripts/package_local_assistant.sh to bundle with Textual TUI (dagents-cli).
EOF
  else
    cat > "${OUT_DIR}/README.txt" <<'EOF'
DAgents Agent Node (Go static build)

完整本地助手请使用 scripts/package_local_assistant.sh（含 Textual TUI）。
EOF
  fi
fi

echo "[done] ${OUT_DIR}"
