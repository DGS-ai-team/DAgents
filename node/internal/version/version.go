// Package version 暴露 Agent Node 构建版本号。
package version

// Version 为对外 /health 与日志使用的语义化版本。
// Release builds inject the canonical VERSION file through -ldflags.
var Version = "dev"
