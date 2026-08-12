package webui

import "embed"

// staticFS 嵌入 node/webui/build.sh 产出目录（node/internal/webui/static，不入库）。
//
//go:embed all:static
var staticFS embed.FS
