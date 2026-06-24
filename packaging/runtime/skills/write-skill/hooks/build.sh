#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$ROOT/../../../../.." && pwd)"
OUT="${1:-$ROOT/protect-loaded-skill.so}"
PLUGIN_DIR="$REPO_ROOT/node/plugins/protect-loaded-skill"
cd "$PLUGIN_DIR"
go build -buildmode=plugin -o "$OUT" .
