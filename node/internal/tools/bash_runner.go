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
	userTimeout    bool // 模型显式传入 timeout_seconds 时为 true（超时可降后台）
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
		out, stats, err := runShellUntilDoneWithRegistry(r, ctx, params)
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
		userTimeout:    userTimeout,
		outputEncoding: resolveShellOutputEncoding(st, encConfigured),
		compress:       r.bashCompress.normalized(),
	}, "", nil
}

func (r *Registry) hardLimitSec() int {
	if r != nil && r.bashHardLimitSec > 0 {
		return r.bashHardLimitSec
	}
	return maxBashTimeoutSec
}

func (r *Registry) startShellCommand(params shellRunParams) (*exec.Cmd, error) {
	cmd, err := buildShellCommand(params.shellType, params.command)
	if err != nil {
		return nil, err
	}
	applyShellCmdDir(cmd, params.cwd)
	return cmd, nil
}

// runShellUntilDoneWithRegistry 在 ctx 有效期内等待 shell 结束。
func runShellUntilDoneWithRegistry(r *Registry, ctx context.Context, params shellRunParams) (string, *OutputCompressStats, error) {
	base, err := r.startShellCommand(params)
	if err != nil {
		return "", nil, err
	}
	cmd := exec.CommandContext(ctx, base.Args[0], base.Args[1:]...)
	cmd.Dir = base.Dir
	cmd.SysProcAttr = base.SysProcAttr
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return "", nil, err
	}
	tree, treeErr := attachProcessTree(cmd)
	if treeErr != nil {
		tree = nil
	}
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	var runErr error
	select {
	case runErr = <-waitErr:
	case <-ctx.Done():
		terminateProcessTree(cmd, tree)
		runErr = <-waitErr
	}
	closeProcessTree(tree)
	outText := decodeShellOutput(stdout.Bytes(), params.outputEncoding)
	errText := decodeShellOutput(stderr.Bytes(), params.outputEncoding)
	if params.shellType == shellPowerShell {
		errText = decodePowerShellCLIXML(errText)
	}
	out, stats := formatShellCompletedOutput(params, outText, errText, cmd.ProcessState, runErr)
	return out, stats, nil
}

// runShellSyncWithAutoDegrade 同步等待；显式 timeout 到期可降后台，未传 timeout 则硬上限杀进程。
// 等待期间可通过 syncShellGate 接受 UI 的终止 / 转后台请求。
func runShellSyncWithAutoDegrade(r *Registry, ctx context.Context, params shellRunParams) (string, *OutputCompressStats, error) {
	cmd, err := r.startShellCommand(params)
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

	tree, treeErr := attachProcessTree(cmd)
	if treeErr != nil {
		tree = nil
	}

	sessionID := sessionIDFromContext(ctx)
	toolCallID := toolCallIDFromContext(ctx)
	job := &backgroundJob{
		id:                 newJobID(),
		sessionID:          sessionID,
		toolName:           "bash_run",
		toolCallID:         toolCallID,
		status:             "running",
		startedAt:          nowMs(),
		done:               make(chan struct{}),
		bashCmd:            cmd,
		processTree:        tree,
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
			job:        job,
			gate:       gate,
		})
		defer r.syncShells.remove(toolCallID)
	}

	collectDone := r.startShellOutputCollector(job, params, stdoutPipe, stderrPipe)

	timer := time.NewTimer(time.Duration(params.timeoutSec) * time.Second)
	defer timer.Stop()

	select {
	case <-collectDone:
		job.mu.Lock()
		status := job.status
		preset := job.result
		result, stats := formatShellCompletedOutput(params, job.bashStdout, job.bashStderr, cmd.ProcessState, nil)
		if status != jobStatusCancelled {
			job.compressStats = stats
		}
		job.mu.Unlock()
		if status == jobStatusCancelled {
			if strings.Contains(preset, "硬上限") {
				return formatShellHardTimeoutResult(params.timeoutSec), nil, nil
			}
			return formatShellCancelledResult(job, params), nil, nil
		}
		return result, stats, nil
	case <-gate.bgCh:
		// 与 collector 并发：先置 autoDegraded，若进程已结束则立即回灌，避免丢 async 回调。
		r.markAutoDegradedAndMaybeNotify(job)
		r.syncShells.remove(toolCallID)
		return formatShellRunningResult(job, params, "user"), nil, nil
	case <-gate.cancelCh:
		job.mu.Lock()
		job.transitionStatusLocked(jobStatusCancelled, "cancelled")
		job.mu.Unlock()
		terminateProcessTree(cmd, tree)
		<-collectDone
		return formatShellCancelledResult(job, params), nil, nil
	case <-timer.C:
		if params.userTimeout {
			r.markAutoDegradedAndMaybeNotify(job)
			r.syncShells.remove(toolCallID)
			return formatShellRunningResult(job, params, "timeout"), nil, nil
		}
		job.mu.Lock()
		job.transitionStatusLocked(jobStatusCancelled, formatShellHardTimeoutResult(params.timeoutSec))
		job.mu.Unlock()
		terminateProcessTree(cmd, tree)
		<-collectDone
		return formatShellHardTimeoutResult(params.timeoutSec), nil, nil
	case <-ctx.Done():
		job.mu.Lock()
		job.transitionStatusLocked(jobStatusCancelled, ctx.Err().Error())
		job.mu.Unlock()
		terminateProcessTree(cmd, tree)
		<-collectDone
		return "", nil, ctx.Err()
	}
}

func (r *Registry) startShellOutputCollector(job *backgroundJob, params shellRunParams, stdoutPipe, stderrPipe io.Reader) chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			closeProcessTree(job.processTree)
			close(job.done)
		}()
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
		if params.shellType == shellPowerShell {
			job.bashStderr = decodePowerShellCLIXML(job.bashStderr)
		}
		if stdoutErr != nil && job.bashStdout == "" {
			job.bashStdout = stdoutErr.Error()
		}
		if stderrErr != nil && job.bashStderr == "" {
			job.bashStderr = stderrErr.Error()
		}
		if job.status == jobStatusCancelled {
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
		result, stats := formatShellCompletedOutput(params, job.bashStdout, job.bashStderr, job.bashCmd.ProcessState, waitErr)
		if code == 0 {
			job.transitionStatusLocked(jobStatusSucceeded, result)
		} else {
			job.transitionStatusLocked(jobStatusFailed, result)
		}
		job.compressStats = stats
		autoDegraded := job.autoDegraded
		job.mu.Unlock()

		if autoDegraded && r.bgJobs != nil {
			r.bgJobs.notifyJobDone(job)
		}
	}()
	return done
}

// markAutoDegradedAndMaybeNotify 标记同步 bash 已转入后台；若 collector 已先完成则立刻回灌。
func (r *Registry) markAutoDegradedAndMaybeNotify(job *backgroundJob) {
	if job == nil {
		return
	}
	job.mu.Lock()
	job.autoDegraded = true
	finished := job.finishedAt != 0
	job.mu.Unlock()
	if r.bgJobs != nil {
		r.bgJobs.put(job)
		if finished {
			r.bgJobs.notifyJobDone(job)
		}
	}
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
	outText, outMeta := sanitizeBashStream(cfg, stdout)
	errText, errMeta := sanitizeBashStream(cfg, stderr)
	stats := aggregateBashCompressStats(outMeta, errMeta)

	header := fmt.Sprintf("[BASH_RESULT] exit=%d", exitCode)

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
