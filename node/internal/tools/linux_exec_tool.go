package tools

import (
	"bytes"
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
	ChannelID      string `json:"channel_id"`
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
			Description: "在指定的 Linux SSH channel 上执行一次非交互命令，返回 stdout、stderr 和退出码。每次调用使用独立 SSH session，不共享 cwd、环境变量或后台进程。执行可能涉及远程写入，必须遵循当前 Agent 的审批策略。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"channel_id":       map[string]any{"type": "string", "description": "已绑定到当前 Agent 的 Linux channel ID。"},
					"command":          map[string]any{"type": "string", "description": "在远程 Linux 主机上执行的 shell 命令。"},
					"cwd":              map[string]any{"type": "string", "description": "可选的远程工作目录；省略时使用 channel 默认目录。"},
					"timeout_ms":       map[string]any{"type": "integer", "minimum": 1, "maximum": maxLinuxExecTimeoutMS, "description": "可选的命令超时，单位毫秒。"},
					"max_output_bytes": map[string]any{"type": "integer", "minimum": 1, "maximum": maxLinuxExecOutputBytes, "description": "可选的 stdout/stderr 单独上限。"},
				},
				"required":             []string{"channel_id", "command"},
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
	args.ChannelID = strings.TrimSpace(args.ChannelID)
	args.Command = strings.TrimSpace(args.Command)
	if args.ChannelID == "" || args.Command == "" {
		return "", fmt.Errorf("channel_id and command are required")
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
		Target: ExecutionTarget{Kind: executionTargetLinuxChannel, ID: args.ChannelID},
		Context: ExecutionContext{
			AgentID:    r.agentID,
			SessionID:  sessionIDFromContext(ctx),
			ToolCallID: toolCallIDFromContext(ctx),
			Target:     ExecutionTarget{Kind: executionTargetLinuxChannel, ID: args.ChannelID},
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
	var outBuf, errBuf boundedOutputBuffer
	outBuf.limit, errBuf.limit = maxOutput, maxOutput
	copyDone := make(chan struct{})
	go func() {
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { _, _ = io.Copy(&outBuf, stdout); wg.Done() }()
		go func() { _, _ = io.Copy(&errBuf, stderr); wg.Done() }()
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
	return formatLinuxExecResult(outBuf.Bytes(), errBuf.Bytes(), process.ExitStatus(), waitErr, outBuf.truncated || errBuf.truncated), nil
}

type boundedOutputBuffer struct {
	bytes.Buffer
	limit     int
	truncated bool
}

func (b *boundedOutputBuffer) Write(data []byte) (int, error) {
	if b.limit <= 0 {
		return len(data), nil
	}
	remaining := b.limit - b.Len()
	if remaining <= 0 {
		b.truncated = true
		return len(data), nil
	}
	if len(data) > remaining {
		_, _ = b.Buffer.Write(data[:remaining])
		b.truncated = true
		return len(data), nil
	}
	return b.Buffer.Write(data)
}

func formatLinuxExecResult(stdout, stderr []byte, exit *ExitStatus, waitErr error, truncated bool) string {
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
	return strings.Join(parts, "\n")
}
