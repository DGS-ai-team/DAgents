#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
OUT="${1:-$ROOT/protect-loaded-skill.so}"
cd "$ROOT/protect-loaded-skill"
go mod download
go build -buildmode=plugin -o "$OUT" .
