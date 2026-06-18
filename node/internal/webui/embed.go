package webui

import "embed"

// staticFS 嵌入 node/webui/build.sh 产出目录（node/internal/webui/static）。
//
//go:embed all:static
var staticFS embed.FS
