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
if [[ -z "${VERSION:-}" ]]; then
  VERSION="$(tr -d '[:space:]' < "${REPO_ROOT}/VERSION")"
fi
if [[ -z "${VERSION}" ]]; then
  echo "[build] VERSION is empty; pass VERSION or populate ${REPO_ROOT}/VERSION" >&2
  exit 1
fi
LDFLAGS="${LDFLAGS} -X github.com/DGS-ai-team/DAgents/node/internal/version.Version=${VERSION}"

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

1. copy config.example.yaml to config.yaml (Node bootstrap: listen only)
2. bin\dagents-node.exe -config config.yaml
3. Connect DAgents Desktop to the Node API at http://127.0.0.1:<listen.port> (default 18765)
4. bin\dagents-client.exe -config config.yaml probe
   bin\dagents-client.exe -config config.yaml update --check

See docs/development.md
EOF
  else
    cat > "${OUT_DIR}/README.txt" <<'EOF'
DAgents Agent Node + Client (Go static build)

1. cp config.example.yaml config.yaml（Node bootstrap：仅 listen）
2. ./bin/dagents-node -config config.yaml
3. 在 DAgents Desktop 中连接 Node API：http://127.0.0.1:<listen.port>（默认 18765）
4. ./bin/dagents-client -config config.yaml probe
   ./bin/dagents-client -config config.yaml update --check

文档: docs/development.md
EOF
  fi
else
  if [[ "${GOOS}" == "windows" ]]; then
    cat > "${OUT_DIR}/README.txt" <<'EOF'
DAgents Agent Node (Go static build, Windows)

Use scripts/package_local_assistant.sh to bundle the Node, clients, browser service, and Windows Desktop Shell.
EOF
  else
    cat > "${OUT_DIR}/README.txt" <<'EOF'
DAgents Agent Node (Go static build)

完整本地助手请使用 scripts/package_local_assistant.sh（包含独立 Desktop UI）。
EOF
  fi
fi

echo "[done] ${OUT_DIR}"
