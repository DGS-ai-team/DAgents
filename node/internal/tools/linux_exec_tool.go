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
	defaultLinuxExecTimeoutMS = 120000
	maxLinuxExecTimeoutMS     = 600000
	maxLinuxExecOutputBytes   = 1 << 20
)

type linuxExecArgs struct {
	ConfigID       string `json:"config_id"`
	ChannelID      string `json:"channel_id"` // legacy compatibility
	Command        string `json:"command"`
	CWD            string `json:"cwd"`
	TimeoutMS      int    `json:"timeout_ms"`
	MaxOutputBytes int    `json:"max_output_bytes"`
}

func linuxExecToolDef() []ToolDef {
	return []ToolDef{{
		Type: "function",
		Function: FunctionDef{
			Name:        "linux_exec",
			Description: "使用 terminal_config_list 返回的 config_id，在指定 Linux SSH 配置上执行一次非交互命令并返回 stdout、stderr 和退出码。每次调用使用独立会话，不保留 cwd、环境或后台进程；需要交互或保持状态时请使用 terminal_open。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"config_id":        map[string]any{"type": "string", "description": "terminal_config_list 返回的 Linux 配置 ID。"},
					"command":          map[string]any{"type": "string", "description": "在远程 Linux 主机上执行的 shell 命令。"},
					"cwd":              map[string]any{"type": "string", "description": "可选的远程工作目录；省略时使用 channel 默认目录。"},
					"timeout_ms":       map[string]any{"type": "integer", "minimum": 1, "maximum": maxLinuxExecTimeoutMS, "description": "可选的命令超时，单位毫秒。"},
					"max_output_bytes": map[string]any{"type": "integer", "minimum": 1, "maximum": maxLinuxExecOutputBytes, "description": "可选的 stdout/stderr 单独上限。"},
				},
				"required":             []string{"config_id", "command"},
				"additionalProperties": false,
			}),
		},
	}}
}

func (r *Registry) execLinuxExec(ctx context.Context, raw json.RawMessage) (string, error) {
	if r == nil || r.linuxProvider == nil {
		return "", fmt.Errorf("linux execution is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var args linuxExecArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	requestedID, err := resolveLinuxToolID(args.ConfigID, args.ChannelID)
	if err != nil {
		return "", err
	}
	channelID, err := r.resolveLinuxChannelID(ctx, requestedID)
	if err != nil {
		return "", err
	}
	args.Command = strings.TrimSpace(args.Command)
	if channelID == "" || args.Command == "" {
		return "", fmt.Errorf("config_id and command are required")
	}
	timeout := args.TimeoutMS
	if timeout <= 0 {
		timeout = defaultLinuxExecTimeoutMS
	}
	if timeout > maxLinuxExecTimeoutMS {
		timeout = maxLinuxExecTimeoutMS
	}
	maxOutput := args.MaxOutputBytes
	if maxOutput <= 0 || maxOutput > maxLinuxExecOutputBytes {
		maxOutput = maxLinuxExecOutputBytes
	}

	process, err := r.linuxProvider.Start(ctx, ExecRequest{
		Target: ExecutionTarget{Kind: executionTargetLinuxChannel, ID: channelID},
		Context: ExecutionContext{
			AgentID:         r.agentID,
			SessionID:       sessionIDFromContext(ctx),
			ToolCallID:      toolCallIDFromContext(ctx),
			BackgroundJobID: BackgroundJobIDFromContext(ctx),
			ApprovalID:      ApprovalIDFromContext(ctx),
			CommandDigest:   executionCommandDigest(args.Command),
			Target:          ExecutionTarget{Kind: executionTargetLinuxChannel, ID: channelID},
		},
		Command:        args.Command,
		CWD:            args.CWD,
		Timeout:        time.Duration(timeout) * time.Millisecond,
		MaxOutputBytes: maxOutput,
		EventSink:      r.processEventSink,
	})
	if err != nil {
		return "", err
	}
	stdout, err := process.StdoutPipe()
	if err != nil {
		_ = process.Close()
		return "", err
	}
	stderr, err := process.StderrPipe()
	if err != nil {
		_ = process.Close()
		return "", err
	}
	if err := process.Start(); err != nil {
		_ = process.Close()
		return "", err
	}
	r.bindBackgroundProcess(ctx, process)
	outBuf := NewOutputBudget(maxOutput)
	errBuf := NewOutputBudget(maxOutput)
	copyDone := make(chan struct{})
	go func() {
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { _, _ = io.Copy(outBuf, stdout); wg.Done() }()
		go func() { _, _ = io.Copy(errBuf, stderr); wg.Done() }()
		wg.Wait()
		close(copyDone)
	}()
	waitDone := make(chan error, 1)
	go func() { waitDone <- process.Wait() }()
	cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
	defer cancel()
	var waitErr error
	select {
	case waitErr = <-waitDone:
	case <-cmdCtx.Done():
		_ = process.Terminate(cmdCtx)
		waitErr = <-waitDone
	}
	<-copyDone
	_ = process.Close()
	terminationState := ""
	if stateProvider, ok := process.(interface{ TerminationState() string }); ok {
		terminationState = stateProvider.TerminationState()
	}
	return formatLinuxExecResult(outBuf.Bytes(), errBuf.Bytes(), process.ExitStatus(), waitErr, outBuf.truncated || errBuf.truncated, terminationState), nil
}

func formatLinuxExecResult(stdout, stderr []byte, exit *ExitStatus, waitErr error, truncated bool, terminationState string) string {
	code := 1
	if exit != nil {
		code = exit.Code
	} else if waitErr == nil {
		code = 0
	}
	parts := []string{
		fmt.Sprintf("[LINUX_RESULT] exit=%d", code),
		"--- STDOUT ---",
		string(stdout),
		"--- STDERR ---",
		string(stderr),
	}
	if truncated {
		parts = append(parts, "output_truncated: true")
	}
	if waitErr != nil && (exit == nil || exit.Error == "") {
		parts = append(parts, "exit_error: "+waitErr.Error())
	}
	if strings.TrimSpace(terminationState) != "" {
		parts = append(parts, "termination_status: "+strings.TrimSpace(terminationState))
	}
	return strings.Join(parts, "\n")
}
