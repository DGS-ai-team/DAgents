package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type backgroundExecContextKey struct{}

type shellRunParams struct {
	command        string
	cwd            string
	shellType      shellType
	timeoutSec     int
	outputEncoding string
	compress       BashCompressConfig
}

// WithBackgroundExecution 标记当前 Execute 由 StartBackground 发起，bash_run 不做同步窗口超时。
func WithBackgroundExecution(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, backgroundExecContextKey{}, true)
}

func isBackgroundExecution(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, ok := ctx.Value(backgroundExecContextKey{}).(bool)
	return ok && v
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

	if isBackgroundExecution(ctx) {
		out, stats, err := runShellUntilDone(ctx, params)
		if err != nil {
			return fmt.Sprintf("ERROR: %v", err), nil
		}
		r.stashBashCompressStats(toolCallIDFromContext(ctx), stats)
		return out, nil
	}
	out, stats, err := runShellSyncWithAutoDegrade(r, ctx, params)
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
	timeout := r.bashTimeout
	if args.TimeoutSeconds != nil && *args.TimeoutSeconds > 0 {
		timeout = *args.TimeoutSeconds
	}
	if timeout > maxBashTimeoutSec {
		timeout = maxBashTimeoutSec
	}
	if timeout < 1 {
		timeout = defaultBashTimeoutSec
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
	return shellRunParams{
		command:        cmdText,
		cwd:            cwd,
		shellType:      st,
		timeoutSec:     timeout,
		outputEncoding: resolveShellOutputEncoding(st, r.shellOutputEncoding),
		compress:       r.bashCompress.normalized(),
	}, "", nil
}

func startShellCommand(params shellRunParams) (*exec.Cmd, error) {
	cmd, err := buildShellCommand(params.shellType, params.command)
	if err != nil {
		return nil, err
	}
	applyShellCmdDir(cmd, params.cwd)
	return cmd, nil
}

// runShellUntilDone 在 ctx 有效期内等待 shell 结束（用于 run_in_background=true）。
func runShellUntilDone(ctx context.Context, params shellRunParams) (string, *OutputCompressStats, error) {
	base, err := startShellCommand(params)
	if err != nil {
		return "", nil, err
	}
	cmd := exec.CommandContext(ctx, base.Args[0], base.Args[1:]...)
	cmd.Dir = base.Dir
	cmd.SysProcAttr = base.SysProcAttr
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	outText := decodeShellOutput(stdout.Bytes(), params.outputEncoding)
	errText := decodeShellOutput(stderr.Bytes(), params.outputEncoding)
	out, stats := formatShellCompletedOutput(params, outText, errText, cmd.ProcessState, runErr)
	return out, stats, nil
}

// runShellSyncWithAutoDegrade 同步等待 timeout 秒；超时则不杀进程并登记后台 job。
func runShellSyncWithAutoDegrade(r *Registry, ctx context.Context, params shellRunParams) (string, *OutputCompressStats, error) {
	cmd, err := startShellCommand(params)
	if err != nil {
		return fmt.Sprintf("ERROR: %v", err), nil, nil
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", nil, fmt.Errorf("bash_run 失败: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", nil, fmt.Errorf("bash_run 失败: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Sprintf("ERROR: bash_run 失败: %v", err), nil, nil
	}

	sessionID := sessionIDFromContext(ctx)
	job := &backgroundJob{
		id:                 newJobID(),
		sessionID:          sessionID,
		toolName:           "bash_run",
		status:             "running",
		startedAt:          nowMs(),
		done:               make(chan struct{}),
		bashCmd:            cmd,
		bashCwd:            params.cwd,
		bashTimeout:        params.timeoutSec,
		bashShellType:      string(params.shellType),
		bashOutputEncoding: params.outputEncoding,
	}

	collectDone := r.startShellOutputCollector(job, params, stdoutPipe, stderrPipe)

	timer := time.NewTimer(time.Duration(params.timeoutSec) * time.Second)
	defer timer.Stop()

	select {
	case <-collectDone:
		job.mu.Lock()
		result, stats := formatShellCompletedOutput(params, job.bashStdout, job.bashStderr, cmd.ProcessState, nil)
		job.compressStats = stats
		job.mu.Unlock()
		return result, stats, nil
	case <-timer.C:
		job.autoDegraded = true
		r.bgJobs.put(job)
		return formatShellRunningResult(job, params), nil, nil
	case <-ctx.Done():
		killShellProcess(cmd)
		return "", nil, ctx.Err()
	}
}

func (r *Registry) startShellOutputCollector(job *backgroundJob, params shellRunParams, stdoutPipe, stderrPipe io.Reader) chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer close(job.done)
		var wg sync.WaitGroup
		wg.Add(2)
		var stdoutBuf, stderrBuf bytes.Buffer
		var stdoutErr, stderrErr error
		go func() {
			_, stdoutErr = io.Copy(&stdoutBuf, stdoutPipe)
			wg.Done()
		}()
		go func() {
			_, stderrErr = io.Copy(&stderrBuf, stderrPipe)
			wg.Done()
		}()
		wg.Wait()
		waitErr := job.bashCmd.Wait()

		job.mu.Lock()
		job.bashStdout = decodeShellOutput(stdoutBuf.Bytes(), params.outputEncoding)
		job.bashStderr = decodeShellOutput(stderrBuf.Bytes(), params.outputEncoding)
		if stdoutErr != nil && job.bashStdout == "" {
			job.bashStdout = stdoutErr.Error()
		}
		if stderrErr != nil && job.bashStderr == "" {
			job.bashStderr = stderrErr.Error()
		}
		if job.status == "cancelled" {
			if job.finishedAt == 0 {
				job.finishedAt = nowMs()
			}
			job.mu.Unlock()
			return
		}
		code := 0
		if job.bashCmd.ProcessState != nil {
			code = job.bashCmd.ProcessState.ExitCode()
		} else if waitErr != nil {
			code = 1
		}
		job.bashExitCode = &code
		if code == 0 {
			job.status = "succeeded"
		} else {
			job.status = "failed"
		}
		job.result, job.compressStats = formatShellCompletedOutput(params, job.bashStdout, job.bashStderr, job.bashCmd.ProcessState, waitErr)
		job.finishedAt = nowMs()
		autoDegraded := job.autoDegraded
		session := job.sessionID
		job.mu.Unlock()

		if autoDegraded && r.bgJobs != nil {
			r.bgJobs.notifyDone(session, jobDonePayload(job))
		}
	}()
	return done
}

func formatShellCompletedOutput(params shellRunParams, stdout, stderr string, state *os.ProcessState, runErr error) (string, *OutputCompressStats) {
	exitCode := 0
	if runErr != nil {
		exitCode = 1
	}
	if state != nil {
		exitCode = state.ExitCode()
	}

	cfg := params.compress.normalized()
	outText, outMeta := compressBashStream(cfg, stdout, cfg.MaxOutputChars)
	errLimit := stderrMaxRunes(cfg, exitCode)
	errText, errMeta := compressBashStream(cfg, stderr, errLimit)
	stats := aggregateBashCompressStats(outMeta, errMeta)

	header := fmt.Sprintf("[BASH_RESULT] exit=%d", exitCode)
	if outMeta.runeTruncated || errMeta.runeTruncated {
		header += " truncated"
	}

	parts := []string{
		header,
		"--- STDOUT ---",
		outText,
		"--- STDERR ---",
		errText,
	}
	if runErr != nil {
		parts = append(parts, "exit_error: "+runErr.Error())
	}
	return strings.Join(parts, "\n"), stats
}

func formatShellRunningResult(job *backgroundJob, params shellRunParams) string {
	st := params.shellType
	if job.bashShellType != "" {
		st = shellType(job.bashShellType)
	}
	return strings.Join([]string{
		fmt.Sprintf("[BASH_RESULT] status=RUNNING job_id=%s", job.id),
		fmt.Sprintf("shell_type=%s", st),
		"命令超过同步等待时间，已自动降级为后台任务；也可显式使用 run_in_background=true。",
		"可用 background_job_status / background_job_cancel 查询或取消。",
	}, "\n")
}
