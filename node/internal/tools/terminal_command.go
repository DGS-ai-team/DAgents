package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	defaultTerminalCommandTimeout = 120 * time.Second
	maxTerminalCommandTimeout     = 10 * time.Minute
	maxTerminalCommandOutputBytes = 1 << 20
)

type terminalCommandArgs struct {
	TerminalID     string `json:"terminal_id"`
	Command        string `json:"command"`
	CWD            string `json:"cwd"`
	TimeoutMS      int    `json:"timeout_ms"`
	MaxOutputBytes int    `json:"max_output_bytes"`
}

func terminalCommandToolDef() ToolDef {
	return ToolDef{Type: "function", Function: FunctionDef{
		Name:        "terminal_command",
		Description: "在已通过 terminal_open 打开的终端目标上执行一次非交互命令。必须传入 terminal_id；命令会复用该终端的现有 PTY/SSH 会话，不会新建第二个连接。目标、Agent 权限和 Linux 通道由已打开的会话绑定，不能自行传 config_id 或 channel_id。结果返回结构化 status、exit_code、stdout、stderr、字节数和截断标记；需要保持交互状态时改用 terminal_input 与 terminal_read。",
		Parameters: injectCallPurposeParam(objectParams(map[string]any{
			"terminal_id":      map[string]any{"type": "string", "description": "terminal_open 返回的终端 ID"},
			"command":          map[string]any{"type": "string", "description": "在已打开目标的现有 shell 会话上执行的命令"},
			"cwd":              map[string]any{"type": "string", "description": "可选；只覆盖本次命令的工作目录，不改变终端默认 cwd"},
			"timeout_ms":       map[string]any{"type": "integer", "minimum": 1, "maximum": int(maxTerminalCommandTimeout / time.Millisecond), "description": "可选的命令超时，单位毫秒"},
			"max_output_bytes": map[string]any{"type": "integer", "minimum": 1, "maximum": maxTerminalCommandOutputBytes, "description": "可选的命令输出上限"},
		}, "terminal_id", "command")),
	}}
}

func (r *Registry) execTerminalCommand(ctx context.Context, raw json.RawMessage) (string, error) {
	broker, err := r.terminalBrokerOrError()
	if err != nil {
		return "", err
	}
	var args terminalCommandArgs
	if err := decodeTerminalArgs(raw, &args); err != nil {
		return "", err
	}
	id := strings.TrimSpace(args.TerminalID)
	if id == "" {
		return "", fmt.Errorf("terminal_id is required")
	}
	args.Command = strings.TrimSpace(args.Command)
	if args.Command == "" {
		return "", fmt.Errorf("command is required")
	}
	info, err := broker.Lookup(r.agentID, id)
	if err != nil {
		return "", err
	}
	if info.Status != "running" {
		return "", fmt.Errorf("terminal session %q is %s; open a new terminal first", id, info.Status)
	}
	timeout := defaultTerminalCommandTimeout
	if args.TimeoutMS > 0 {
		timeout = time.Duration(args.TimeoutMS) * time.Millisecond
	}
	if timeout > maxTerminalCommandTimeout {
		timeout = maxTerminalCommandTimeout
	}
	maxOutput := args.MaxOutputBytes
	if maxOutput <= 0 || maxOutput > maxTerminalCommandOutputBytes {
		maxOutput = maxTerminalCommandOutputBytes
	}
	result, err := broker.RunCommand(ctx, r.agentID, id, TerminalCommandRequest{
		TerminalID:     id,
		Target:         ExecutionTarget{Kind: info.TargetKind, ID: info.TargetID},
		ConfigID:       info.ConfigID,
		Shell:          info.Shell,
		CWD:            strings.TrimSpace(args.CWD),
		Command:        args.Command,
		Timeout:        timeout,
		MaxOutputBytes: maxOutput,
	})
	if err != nil {
		return "", err
	}
	return marshalTerminalResult(result)
}
