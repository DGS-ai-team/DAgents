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
	OutputEncoding *string `json:"output_encoding"`
}

func bashRunToolDef() ToolDef {
	isWindows := hostsnapshot.Get().OSKind == "windows"
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "bash_run",
			Description: bashRunToolDescription(isWindows),
			Parameters: injectCallPurposeParam(map[string]any{
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
						"description": "同步等待秒数（可选，1-600）。显式传入时超时自动降为后台 job；省略则最长等待硬上限后终止（不转后台）。",
					},
					"cwd": map[string]any{
						"type":        "string",
						"description": "执行目录（可选，默认在工作区根目录下）",
					},
					"shell_type": map[string]any{
						"type":        "string",
						"enum":        []string{"bash", "cmd", "powershell"},
						"description": bashRunShellTypeParamDescription(isWindows),
					},
					"output_encoding": map[string]any{
						"type":        "string",
						"description": "stdout/stderr 字节编码（可选，如 utf-8、gbk；默认按 shell 与 tools.bash_output_encoding 自动选择）",
					},
				},
				"required":             []string{"command"},
				"additionalProperties": false,
			}),
		},
	}
}

func bashRunToolDescription(isWindows bool) string {
	tail := "外置 CLI、编译二进制与 shell 脚本见 `externaltools/` 与根目录 `externaltools_menu.md`（可通过命令名或 `externaltools/<tool>` 调用）。" +
		" 除非明确需要，否则避免 su/sudo 等需交互密码的命令。" +
		" 若传入 timeout_seconds，同步等待该秒数，超时自动降为后台 job（返回 job_id），完成后自动回灌；" +
		" 省略 timeout_seconds 时最长等待硬上限（默认 600 秒），超时终止并返回错误（不转后台）。" +
		" 用户可在 UI 对进行中的 bash 手动终止或转后台。" +
		" 长输出会按 tools.bash_compress 清洗；超长结果落盘并在 history 中头尾摘要（hooks.tool_result）。"
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
