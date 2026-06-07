package tools

const (
	defaultBashTimeoutSec = 30
	maxBashTimeoutSec     = 600
	maxBashOutputChars    = 12000
)

type bashRunArgs struct {
	Command        string  `json:"command"`
	TimeoutSeconds *int    `json:"timeout_seconds"`
	Cwd            *string `json:"cwd"`
	ShellType      *string `json:"shell_type"`
}

func bashRunToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name: "bash_run",
			Description: "执行 bash/cmd/powershell 命令；同步超时自动降级后台，或显式 run_in_background=true",
			Parameters: injectRunInBackgroundParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{
						"type":        "string",
						"description": "命令字符串（必填）",
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
						"description": "目标 shell（可选）；省略时 Windows=powershell，其它=bash",
					},
				},
				"required":             []string{"command"},
				"additionalProperties": false,
			}),
		},
	}
}
