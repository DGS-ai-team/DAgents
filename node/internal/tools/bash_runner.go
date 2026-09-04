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

type shellRunParams struct {
	command        string
	cwd            string
	shellType      shellType
	timeoutSec     int
	outputEncoding string
	compress       BashCompressConfig
}

func (r *Registry) execBashRun(ctx context.Context, raw json.RawMessage) (string, error) {
	var args bashRunArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	params, errMsg, err := r.prepareShellRun(args)
	if err != nil {
		return "", err
	}
	if errMsg != "" {
		return errMsg, nil
	}

	out, stats, err := runShellSync(r, ctx, params)
	if err != nil {
		return "", err
	}
	r.stashBashCompressStats(toolCallIDFromContext(ctx), stats)
	return out, nil
}

func (r *Registry) prepareShellRun(args bashRunArgs) (shellRunParams, string, error) {
	cmdText := strings.TrimSpace(args.Command)
	if cmdText == "" {
		return shellRunParams{}, "ERROR: command 不能为空。", nil
	}
	userTimeout := args.TimeoutSeconds != nil && *args.TimeoutSeconds > 0
	timeout := r.hardLimitSec()
	if userTimeout {
		timeout = *args.TimeoutSeconds
	}
	if timeout > maxBashTimeoutSec {
		timeout = maxBashTimeoutSec
	}
	if timeout < 1 {
		if userTimeout {
			timeout = defaultBashTimeoutSec
		} else {
			timeout = maxBashTimeoutSec
		}
	}
	cwdRaw := ""
	if args.Cwd != nil {
		cwdRaw = *args.Cwd
	}
	cwd, err := r.resolveRunCWD(cwdRaw)
	if err != nil {
		return shellRunParams{}, fmt.Sprintf("ERROR: %v", err), nil
	}
	st, err := resolveShellType(args.ShellType)
	if err != nil {
		return shellRunParams{}, fmt.Sprintf("ERROR: %v", err), nil
	}
	if blocked := blockedNonRootPasswordPromptingShell(cmdText, st); blocked != "" {
		return shellRunParams{}, blocked, nil
	}
	encConfigured := r.shellOutputEncoding
	if args.OutputEncoding != nil {
		encConfigured = strings.TrimSpace(*args.OutputEncoding)
	}
	return shellRunParams{
		command:        cmdText,
		cwd:            cwd,
		shellType:      st,
		timeoutSec:     timeout,
		outputEncoding: resolveShellOutputEncoding(encConfigured),
		compress:       r.bashCompress.normalized(),
	}, "", nil
}

func (r *Registry) hardLimitSec() int {
	if r != nil && r.bashHardLimitSec > 0 {
		return r.bashHardLimitSec
	}
	return maxBashTimeoutSec
}

func (r *Registry) startShellProcess(ctx context.Context, params shellRunParams) (Process, error) {
	provider := r.shellProvider
	if provider == nil {
		provider = NewLocalShellProvider()
	}
	return provider.Start(ctx, ExecRequest{
		Target: ExecutionTarget{Kind: executionTargetLocal},
		Context: ExecutionContext{
			AgentID:       r.agentID,
			SessionID:     sessionIDFromContext(ctx),
			ToolCallID:    toolCallIDFromContext(ctx),
			CommandDigest: executionCommandDigest(params.command),
			Target:        ExecutionTarget{Kind: executionTargetLocal},
		},
		ShellType: string(params.shellType),
		Command:   params.command,
		CWD:       params.cwd,
		Timeout:   time.Duration(params.timeoutSec) * time.Second,
		EventSink: r.processEventSink,
	})
}

// runShellSync waits for one bash_run process. Timeout and cancellation both
// terminate this process; neither path creates a background job.
func runShellSync(r *Registry, ctx context.Context, params shellRunParams) (string, *OutputCompressStats, error) {
	process, err := r.startShellProcess(ctx, params)
	if err != nil {
		return fmt.Sprintf("ERROR: %v", err), nil, nil
	}
	stdoutPipe, err := process.StdoutPipe()
	if err != nil {
		return "", nil, fmt.Errorf("bash_run 失败: %w", err)
	}
	stderrPipe, err := process.StderrPipe()
	if err != nil {
		return "", nil, fmt.Errorf("bash_run 失败: %w", err)
	}
	if err := process.Start(); err != nil {
		return fmt.Sprintf("ERROR: bash_run 失败: %v", err), nil, nil
	}

	sessionID := sessionIDFromContext(ctx)
	toolCallID := toolCallIDFromContext(ctx)
	execution := &shellExecution{
		status:             shellStatusRunning,
		done:               make(chan struct{}),
		process:            process,
		bashCwd:            params.cwd,
		bashTimeout:        params.timeoutSec,
		bashShellType:      string(params.shellType),
		bashOutputEncoding: params.outputEncoding,
	}
	gate := newSyncShellGate()
	if r.syncShells != nil && strings.TrimSpace(toolCallID) != "" {
		r.syncShells.put(&syncShellEntry{
			sessionID:  sessionID,
			toolCallID: toolCallID,
			gate:       gate,
		})
		defer r.syncShells.remove(toolCallID)
	}

	collectDone := r.startShellOutputCollector(execution, params, stdoutPipe, stderrPipe)

	timer := time.NewTimer(time.Duration(params.timeoutSec) * time.Second)
	defer timer.Stop()

	select {
	case <-collectDone:
		execution.mu.Lock()
		status := execution.status
		preset := execution.result
		result, stats := formatShellCompletedOutput(params, execution.bashStdout, execution.bashStderr, process.ExitStatus(), nil)
		if status != shellStatusCancelled {
			execution.compressStats = stats
		}
		execution.mu.Unlock()
		if status == shellStatusCancelled {
			if strings.Contains(preset, "硬上限") {
				return formatShellHardTimeoutResult(params.timeoutSec), nil, nil
			}
			return formatShellCancelledResult(execution, params), nil, nil
		}
		return result, stats, nil
	case <-gate.cancelCh:
		execution.mu.Lock()
		execution.transitionStatusLocked(shellStatusCancelled, "cancelled")
		execution.mu.Unlock()
		_ = process.Terminate(ctx)
		<-collectDone
		return formatShellCancelledResult(execution, params), nil, nil
	case <-timer.C:
		execution.mu.Lock()
		execution.transitionStatusLocked(shellStatusCancelled, formatShellHardTimeoutResult(params.timeoutSec))
		execution.mu.Unlock()
		_ = process.Terminate(ctx)
		<-collectDone
		return formatShellHardTimeoutResult(params.timeoutSec), nil, nil
	case <-ctx.Done():
		execution.mu.Lock()
		execution.transitionStatusLocked(shellStatusCancelled, ctx.Err().Error())
		execution.mu.Unlock()
		_ = process.Terminate(ctx)
		<-collectDone
		return "", nil, ctx.Err()
	}
}

func (r *Registry) startShellOutputCollector(execution *shellExecution, params shellRunParams, stdoutPipe, stderrPipe io.Reader) chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			_ = execution.process.Close()
			close(execution.done)
		}()
		var wg sync.WaitGroup
		wg.Add(2)
		stdoutBuf := newBashOutputBuffer(params.compress, false)
		stderrBuf := newBashOutputBuffer(params.compress, true)
		var stdoutErr, stderrErr error
		go func() {
			_, stdoutErr = io.Copy(stdoutBuf, stdoutPipe)
			wg.Done()
		}()
		go func() {
			_, stderrErr = io.Copy(stderrBuf, stderrPipe)
			wg.Done()
		}()
		wg.Wait()
		waitErr := execution.process.Wait()

		execution.mu.Lock()
		execution.bashStdout = decodeShellOutput(stdoutBuf.Bytes(), params.outputEncoding)
		execution.bashStderr = decodeShellOutput(stderrBuf.Bytes(), params.outputEncoding)
		execution.bashOutputTruncated = stdoutBuf.truncated || stderrBuf.truncated
		if params.shellType == shellPowerShell {
			execution.bashStderr = decodePowerShellCLIXML(execution.bashStderr)
		}
		if stdoutErr != nil && execution.bashStdout == "" {
			execution.bashStdout = stdoutErr.Error()
		}
		if stderrErr != nil && execution.bashStderr == "" {
			execution.bashStderr = stderrErr.Error()
		}
		if execution.status == shellStatusCancelled {
			if execution.finishedAt == 0 {
				execution.finishedAt = time.Now().UnixMilli()
			}
			execution.mu.Unlock()
			return
		}
		code := 0
		if exit := execution.process.ExitStatus(); exit != nil {
			code = exit.Code
		} else if waitErr != nil {
			code = 1
		}
		execution.bashExitCode = &code
		result, stats := formatShellCompletedOutputWithCapture(params, execution.bashStdout, execution.bashStderr, execution.process.ExitStatus(), waitErr, execution.bashOutputTruncated)
		if code == 0 {
			execution.transitionStatusLocked(shellStatusSucceeded, result)
		} else {
			execution.transitionStatusLocked(shellStatusFailed, result)
		}
		execution.compressStats = stats
		execution.mu.Unlock()
	}()
	return done
}

func formatShellCancelledResult(execution *shellExecution, params shellRunParams) string {
	st := params.shellType
	if execution != nil && execution.bashShellType != "" {
		st = shellType(execution.bashShellType)
	}
	return strings.Join([]string{
		"[BASH_RESULT] status=CANCELLED",
		fmt.Sprintf("shell_type=%s", st),
		"命令已被用户终止。",
	}, "\n")
}

func formatShellHardTimeoutResult(timeoutSec int) string {
	return strings.Join([]string{
		"[BASH_RESULT] status=TIMED_OUT",
		fmt.Sprintf("ERROR: 命令超过 timeout_seconds=%d 仍未结束，已终止。", timeoutSec),
		"bash_run 始终同步执行；如需保持长期运行状态，请使用 terminal_open。",
	}, "\n")
}

func formatShellCompletedOutput(params shellRunParams, stdout, stderr string, exit *ExitStatus, runErr error) (string, *OutputCompressStats) {
	return formatShellCompletedOutputWithCapture(params, stdout, stderr, exit, runErr, false)
}

func formatShellCompletedOutputWithCapture(params shellRunParams, stdout, stderr string, exit *ExitStatus, runErr error, capturedTruncated bool) (string, *OutputCompressStats) {
	exitCode := 0
	if runErr != nil {
		exitCode = 1
	}
	if exit != nil {
		exitCode = exit.Code
	}

	cfg := params.compress.normalized()
	outText, outMeta := compressBashStream(cfg, stdout, cfg.MaxOutputChars)
	errText, errMeta := compressBashStream(cfg, stderr, cfg.MaxOutputCharsStderr)
	stats := aggregateBashCompressStats(outMeta, errMeta)
	if capturedTruncated {
		if stats == nil {
			stats = &OutputCompressStats{
				RawRunes: outMeta.inRunes + errMeta.inRunes,
				OutRunes: outMeta.outRunes + errMeta.outRunes,
				SavedPct: 1,
			}
		}
		stats.Truncated = true
	}

	header := fmt.Sprintf("[BASH_RESULT] exit=%d", exitCode)
	status := "SUCCEEDED"
	if exitCode != 0 || runErr != nil {
		status = "FAILED"
	}

	parts := []string{
		header,
		"status=" + status,
		"target=local",
		fmt.Sprintf("exit_code=%d", exitCode),
		fmt.Sprintf("stdout_bytes=%d", len([]byte(outText))),
		fmt.Sprintf("stderr_bytes=%d", len([]byte(errText))),
		fmt.Sprintf("output_truncated: %t", stats != nil && stats.Truncated),
		"--- STDOUT ---",
		outText,
		"--- STDERR ---",
		errText,
	}
	if runErr != nil {
		parts = append(parts, "exit_error: "+runErr.Error())
	}
	if stats != nil && stats.Truncated {
		parts = append(parts, "output_truncated: true")
	}
	return strings.Join(parts, "\n"), stats
}
