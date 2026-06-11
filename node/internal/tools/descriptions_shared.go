package tools

// 工具 description 中的共用约定（原先散落在 system prompt，现绑定到各工具 schema）。
const (
	descFSPathConvention = "path 均相对 FS_ROOT（`.` = 工作区根）。"
	descReadBeforeWrite  = "修改已有文件前须先 read_file 核对空白、换行与上下文。"
	descPromptContext    = "prompt_context/ 下 soul/user/custom 侧车 Markdown 已注入 system prompt，通常无需 read_file。"
	descScriptsHint      = "可执行脚本见 FS_ROOT/scripts/ 与 scripts_menu.md。"
)
