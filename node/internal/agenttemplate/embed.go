package agenttemplate

import "embed"

// 内置模板随 Node 二进制嵌入，安装包不依赖 cwd 下 packaging/agent-templates。
// 源文件与 packaging/agent-templates/*.yaml 保持同步。
//
//go:embed builtin/*.yaml
var builtinTemplatesFS embed.FS
