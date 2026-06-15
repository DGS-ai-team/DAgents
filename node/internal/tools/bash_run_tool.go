package tools

import (
	"github.com/DGS-ai-team/DAgents/node/internal/hostsnapshot"
)

const (
	defaultBashTimeoutSec    = 30
	maxBashTimeoutSec        = 600
	maxBashOutputRunes       = 12000
	maxBashOutputStderrRunes = 16000
)

type bashRunArgs struct {
	Command        string  `json:"command"`
	TimeoutSeconds *int    `json:"timeout_seconds"`
	Cwd            *string `json:"cwd"`
	ShellType      *string `json:"shell_type"`
}

func bashRunToolDef() ToolDef {
	isWindows := hostsnapshot.Get().OSKind == "windows"
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "bash_run",
			Description: bashRunToolDescription(isWindows),
			Parameters: injectRunInBackgroundParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{
						"type":        "string",
						"description": bashRunCommandParamDescription(isWindows),
					},
					"timeout_seconds": map[string]any{
						"type":        "integer",
						"minimum":     1,
						"maximum":     maxBashTimeoutSec,
						"description": "超时秒数（可选，默认 30，范围 1-600）",
					},
					"cwd": map[string]any{
						"type":        "string",
						"description": "执行目录（可选，默认 fs_root）；须存在且在 FS_ROOT 内",
					},
					"shell_type": map[string]any{
						"type":        "string",
						"enum":        []string{"bash", "cmd", "powershell"},
						"description": bashRunShellTypeParamDescription(isWindows),
					},
				},
				"required":             []string{"command"},
				"additionalProperties": false,
			}),
		},
	}
}

func bashRunToolDescription(isWindows bool) string {
	tail := descScriptsHint +
		" 除非明确需要，否则避免 su/sudo 等需交互密码的命令。" +
		" 同步超时自动降级后台（返回 job_id），或显式 run_in_background=true；长时任务用 background_job_status / background_job_cancel 跟进。" +
		" 长输出会按配置自动清洗与截断（tools.bash_compress）。"
	if isWindows {
		return "执行 PowerShell 命令；cwd 省略时默认为工作区根。" +
			" 当前环境为 Windows，省略 shell_type 时默认 powershell。" +
			" command 须使用 PowerShell 语法（如 Get-ChildItem、Copy-Item），勿使用 cmd.exe 语法（如 dir、copy、type）。" + tail
	}
	return "执行 bash 命令；cwd 省略时默认为工作区根。" +
		" 当前环境非 Windows，省略 shell_type 时默认 bash。" +
		" command 须使用 bash 语法（如 ls、grep、cat），勿使用 PowerShell 或 cmd 语法。" + tail
}

func bashRunCommandParamDescription(isWindows bool) string {
	if isWindows {
		return "PowerShell 命令字符串（必填）；"
	}
	return "bash 命令字符串（必填）；须为 bash 语法"
}

func bashRunShellTypeParamDescription(isWindows bool) string {
	if isWindows {
		return "目标 shell（可选，默认 powershell）；"
	}
	return "目标 shell（可选，默认 bash）；"
}
