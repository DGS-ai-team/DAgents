#!/usr/bin/env bash
# 兼容入口：linux/amd64 静态构建。
exec "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/build_go_static.sh" "$@"
