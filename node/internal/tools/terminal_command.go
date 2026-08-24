package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
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
		Description: "在已通过 terminal_open 打开的终端目标上执行一次非交互命令。必须传入 terminal_id；目标、Agent 权限和 Linux 通道由已打开的会话绑定，不能自行传 config_id 或 channel_id。结果返回结构化 status、exit_code、stdout、stderr、字节数和截断标记；需要保持交互状态时改用 terminal_input 与 terminal_read。",
		Parameters: injectCallPurposeParam(objectParams(map[string]any{
			"terminal_id":      map[string]any{"type": "string", "description": "terminal_open 返回的终端 ID"},
			"command":          map[string]any{"type": "string", "description": "在已打开目标上执行的 shell 命令"},
			"cwd":              map[string]any{"type": "string", "description": "可选；覆盖本次命令的工作目录，不改变终端默认 cwd"},
			"timeout_ms":       map[string]any{"type": "integer", "minimum": 1, "maximum": int(maxTerminalCommandTimeout / time.Millisecond), "description": "可选的命令超时，单位毫秒"},
			"max_output_bytes": map[string]any{"type": "integer", "minimum": 1, "maximum": maxTerminalCommandOutputBytes, "description": "可选的 stdout/stderr 单独上限"},
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
	result, err := r.runTerminalCommand(ctx, TerminalCommandRequest{
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

func (r *Registry) runTerminalCommand(ctx context.Context, req TerminalCommandRequest) (TerminalCommandResult, error) {
	if r == nil {
		return TerminalCommandResult{}, fmt.Errorf("terminal registry is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	provider := r.shellProvider
	if req.Target.Kind == executionTargetLinuxChannel {
		provider = r.linuxProvider
	}
	if provider == nil {
		return TerminalCommandResult{}, fmt.Errorf("terminal command provider is unavailable")
	}
	if req.Target.Kind != "" && req.Target.Kind != executionTargetLocal && req.Target.Kind != executionTargetLinuxChannel {
		return TerminalCommandResult{}, fmt.Errorf("unsupported terminal target %q", req.Target.Kind)
	}
	process, err := provider.Start(ctx, ExecRequest{
		Target: req.Target,
		Context: ExecutionContext{
			AgentID:       r.agentID,
			SessionID:     sessionIDFromContext(ctx),
			ToolCallID:    toolCallIDFromContext(ctx),
			ApprovalID:    ApprovalIDFromContext(ctx),
			CommandDigest: executionCommandDigest(req.Command),
			Target:        req.Target,
		},
		ShellType:      req.Shell,
		Command:        req.Command,
		CWD:            req.CWD,
		Timeout:        req.Timeout,
		MaxOutputBytes: req.MaxOutputBytes,
		EventSink:      r.processEventSink,
	})
	if err != nil {
		return TerminalCommandResult{}, err
	}
	stdout, err := process.StdoutPipe()
	if err != nil {
		_ = process.Close()
		return TerminalCommandResult{}, err
	}
	stderr, err := process.StderrPipe()
	if err != nil {
		_ = stdout.Close()
		_ = process.Close()
		return TerminalCommandResult{}, err
	}
	if err := process.Start(); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		_ = process.Close()
		return TerminalCommandResult{}, err
	}
	outBuf := NewOutputBudget(req.MaxOutputBytes)
	errBuf := NewOutputBudget(req.MaxOutputBytes)
	copyDone := make(chan struct{})
	go func() {
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { _, _ = io.Copy(outBuf, stdout); wg.Done() }()
		go func() { _, _ = io.Copy(errBuf, stderr); wg.Done() }()
		wg.Wait()
		close(copyDone)
	}()
	waitErr, timedOut := waitForProcess(ctx, process, req.Timeout)
	waitForOutputReaders(copyDone, stdout, stderr)
	_ = process.Close()
	exit := process.ExitStatus()
	code := 1
	if exit != nil {
		code = exit.Code
	} else if waitErr == nil {
		code = 0
	}
	status := "SUCCEEDED"
	if code != 0 || waitErr != nil || timedOut {
		status = "FAILED"
	}
	result := TerminalCommandResult{
		Status: status, TerminalID: req.TerminalID, TargetKind: req.Target.Kind, ExitCode: code,
		Stdout: string(outBuf.Bytes()), Stderr: string(errBuf.Bytes()),
		StdoutBytes: len(outBuf.Bytes()), StderrBytes: len(errBuf.Bytes()),
		OutputTruncated: outBuf.truncated || errBuf.truncated, TimedOut: timedOut,
	}
	if exit != nil && exit.Error != "" {
		result.Error = exit.Error
	} else if waitErr != nil && !timedOut {
		result.Error = waitErr.Error()
	} else if timedOut {
		result.Error = "terminal command timed out"
	}
	return result, nil
}
