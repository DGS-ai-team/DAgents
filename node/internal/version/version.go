// Package version 暴露 Agent Node 构建版本号（N0 为常量，后续可由 -ldflags 注入）。
package version

// Version 为对外 /health 与日志使用的语义化版本。
const Version = "0.3.5"
