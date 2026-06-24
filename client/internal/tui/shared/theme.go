package shared

// 终端 ANSI 主题色（transcript / 面板展示层使用，非 lipgloss 渲染区）。

const (
	ThemeUser       = "\033[34m"
	ThemeAssistant  = "\033[32m"
	ThemeReasoning  = "\033[33m"
	ThemeTool       = "\033[36m"
	ThemeSystem     = "\033[90m"
	ThemePanelTitle = "\033[1;36m"
	ThemePanelSection = "\033[36m"
	ThemePanelLabel   = "\033[90m"
	ThemePanelLoaded  = "\033[32m"
	ThemePanelCurrent = "\033[1;33m"
	ThemePanelDim     = "\033[90m"
	ThemeReset        = "\033[0m"

	ThemeSSEOK    = "78"
	ThemeSSEFail  = "203"
	ThemeWarn     = "214"
	ThemeChild    = "39"
	ThemeHelp     = "240"
	ThemeErr      = "203"
	ThemePolicyAllow    = "78"
	ThemePolicyApproval = "214"
	ThemePolicyRule     = "75"
	ThemePolicyDeny     = "203"
)
