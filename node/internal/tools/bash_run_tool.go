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
	common := "结果包含 status、exit_code、stdout_bytes、stderr_bytes 和 output_truncated；exit_code=0 表示进程成功结束，但 stdout 仍可能为空。传入 timeout_seconds 时，超时自动转为后台 job 并返回 job_id；省略时超时终止。长输出会按配置清洗并截断。需要保持目录、环境或进程状态时，请使用 terminal_open。"
	if isWindows {
		return "在 Node 所在 Windows 主机执行一次本地 PowerShell 命令并返回结果。cwd 省略时使用工作区根目录；省略 shell_type 时使用 powershell。" + common
	}
	return "在 Node 所在主机执行一次本地 bash 命令并返回结果。cwd 省略时使用工作区根目录；省略 shell_type 时使用 bash。" + common
}

func bashRunCommandParamDescription(isWindows bool) string {
	if isWindows {
		return "PowerShell 语句（必填）；直接填写命令，不要额外包装 powershell -Command"
	}
	return "bash 命令字符串（必填）；须使用 bash 语法"
}

func bashRunShellTypeParamDescription(isWindows bool) string {
	if isWindows {
		return "目标 shell（可选，默认 powershell）；"
	}
	return "目标 shell（可选，默认 bash）；"
}
