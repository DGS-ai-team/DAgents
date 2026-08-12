package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type backgroundJob struct {
	id         string
	sessionID  string
	toolName   string
	toolCallID string
	mu         sync.Mutex
	status     string // running / succeeded / failed / cancelled
	result     string
	startedAt  int64
	finishedAt int64
	done       chan struct{}
	cancelFn   context.CancelFunc
	// bash 超时自动降级：保留子进程由 collector 收割。
	autoDegraded       bool
	bashCmd            *exec.Cmd
	bashCwd            string
	bashTimeout        int
	bashStdout         string
	bashStderr         string
	bashExitCode       *int
	bashShellType      string
	bashOutputEncoding string
	compressStats      *OutputCompressStats
	// notifyOnce 保证完成/取消只回灌一次（collector、cancel、降级竞态可并发触发）。
	notifyOnce sync.Once
}

// BackgroundJobDone 为后台任务完成时的结构化回灌载荷（由 session 转为 async_tool_result 入队）。
type BackgroundJobDone struct {
	JobID                  string
	ToolName               string
	ToolCallID             string
	Status                 string
	ResultText             string
	ErrorText              string
	OutputCompressSavedPct int
	OutputCompressRawRunes int
	OutputCompressOutRunes int
}

type backgroundJobRegistry struct {
	mu     sync.RWMutex
	jobs   map[string]*backgroundJob
	onDone func(sessionID string, done BackgroundJobDone)
}

// SetBackgroundJobNotifier 注册后台任务完成回调；session 层应转为 async_tool_result 入队。
func (r *Registry) SetBackgroundJobNotifier(fn func(sessionID string, done BackgroundJobDone)) {
	if r.bgJobs == nil {
		return
	}
	r.bgJobs.mu.Lock()
	r.bgJobs.onDone = fn
	r.bgJobs.mu.Unlock()
}

func (reg *backgroundJobRegistry) put(job *backgroundJob) {
	reg.mu.Lock()
	reg.jobs[job.id] = job
	reg.mu.Unlock()
}

func (reg *backgroundJobRegistry) get(id string) (*backgroundJob, bool) {
	reg.mu.RLock()
	job, ok := reg.jobs[strings.TrimSpace(id)]
	reg.mu.RUnlock()
	return job, ok
}

func (reg *backgroundJobRegistry) countRunning(sessionID string) int {
	if reg == nil {
		return 0
	}
	sid := strings.TrimSpace(sessionID)
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	n := 0
	for _, job := range reg.jobs {
		if job == nil {
			continue
		}
		job.mu.Lock()
		running := job.status == "running"
		jobSid := job.sessionID
		job.mu.Unlock()
		if !running {
			continue
		}
		if sid == "" || jobSid == sid {
			n++
		}
	}
	return n
}

func (reg *backgroundJobRegistry) runningCallIDs(sessionID string) []string {
	if reg == nil {
		return nil
	}
	sid := strings.TrimSpace(sessionID)
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	out := make([]string, 0)
	seen := map[string]struct{}{}
	for _, job := range reg.jobs {
		if job == nil {
			continue
		}
		job.mu.Lock()
		running := job.status == "running"
		jobSid := job.sessionID
		callID := strings.TrimSpace(job.toolCallID)
		job.mu.Unlock()
		if !running || callID == "" {
			continue
		}
		if sid != "" && jobSid != "" && jobSid != sid {
			continue
		}
		if _, ok := seen[callID]; ok {
			continue
		}
		seen[callID] = struct{}{}
		out = append(out, callID)
	}
	return out
}

func (reg *backgroundJobRegistry) findRunningByToolCallID(sessionID, toolCallID string) (*backgroundJob, bool) {
	if reg == nil {
		return nil, false
	}
	sid := strings.TrimSpace(sessionID)
	callID := strings.TrimSpace(toolCallID)
	if callID == "" {
		return nil, false
	}
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	for _, job := range reg.jobs {
		if job == nil {
			continue
		}
		job.mu.Lock()
		running := job.status == "running"
		jobSid := job.sessionID
		jobCall := strings.TrimSpace(job.toolCallID)
		job.mu.Unlock()
		if !running || jobCall != callID {
			continue
		}
		if sid != "" && jobSid != "" && jobSid != sid {
			continue
		}
		return job, true
	}
	return nil, false
}

func newBackgroundJobRegistry() *backgroundJobRegistry {
	return &backgroundJobRegistry{jobs: make(map[string]*backgroundJob)}
}

func (reg *backgroundJobRegistry) notifyDone(sessionID string, done BackgroundJobDone) {
	reg.mu.RLock()
	fn := reg.onDone
	reg.mu.RUnlock()
	if fn != nil && sessionID != "" {
		fn(sessionID, done)
	}
}

// notifyJobDone 幂等回灌后台任务完成/取消结果。
func (reg *backgroundJobRegistry) notifyJobDone(job *backgroundJob) {
	if reg == nil || job == nil {
		return
	}
	job.notifyOnce.Do(func() {
		done := jobDonePayload(job)
		job.mu.Lock()
		sessionID := strings.TrimSpace(job.sessionID)
		job.mu.Unlock()
		reg.notifyDone(sessionID, done)
	})
}

// jobDonePayloadLocked 在已持有 job.mu 时读取完成载荷。
func jobDonePayloadLocked(job *backgroundJob) BackgroundJobDone {
	result := job.result
	errText := ""
	if job.status == "failed" || job.status == "cancelled" {
		errText = result
	}
	done := BackgroundJobDone{
		JobID:      job.id,
		ToolName:   job.toolName,
		ToolCallID: job.toolCallID,
		Status:     job.status,
		ResultText: result,
		ErrorText:  errText,
	}
	if job.compressStats != nil {
		done.OutputCompressSavedPct = job.compressStats.SavedPct
		done.OutputCompressRawRunes = job.compressStats.RawRunes
		done.OutputCompressOutRunes = job.compressStats.OutRunes
	}
	return done
}

func jobDonePayload(job *backgroundJob) BackgroundJobDone {
	job.mu.Lock()
	defer job.mu.Unlock()
	return jobDonePayloadLocked(job)
}

func nowMs() int64 {
	return time.Now().UnixMilli()
}

func newJobID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("job-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// StartBackground 在后台 goroutine 执行工具，并立即返回受理 ACK。
// 未启用工具在受理前 soft reject，避免假后台任务。
func (r *Registry) StartBackground(
	parent context.Context,
	sessionID, toolName, toolCallID, cleanedArgs string,
) (string, error) {
	if r.bgJobs == nil {
		return "", fmt.Errorf("background jobs not initialized")
	}
	if err := r.rejectIfDisabled(parent, toolName); err != nil {
		return "", err
	}
	jobCtx, cancel := context.WithCancel(WithBackgroundExecution(WithToolCallID(WithSession(parent, sessionID), toolCallID)))
	job := &backgroundJob{
		id:         newJobID(),
		sessionID:  sessionID,
		toolName:   toolName,
		toolCallID: toolCallID,
		status:     "running",
		startedAt:  nowMs(),
		done:       make(chan struct{}),
		cancelFn:   cancel,
	}
	r.bgJobs.put(job)

	go func() {
		defer close(job.done)
		defer cancel()
		result, err := r.Execute(jobCtx, toolName, cleanedArgs)
		job.mu.Lock()
		if job.status == "cancelled" {
			// cancelJob 已写入终态，不再覆盖。
		} else if jobCtx.Err() == context.Canceled {
			job.status = "cancelled"
			if job.result == "" {
				job.result = "任务已取消。"
			}
		} else if err != nil {
			job.result = err.Error()
			if job.status == "running" {
				job.status = "failed"
			}
		} else {
			job.result = result
			if job.status == "running" {
				job.status = "succeeded"
			}
		}
		if stats := r.TakeBashCompressStatsForCall(toolCallID); stats != nil {
			job.compressStats = outputCompressStatsFromSSEFields(stats)
		}
		job.finishedAt = nowMs()
		job.mu.Unlock()
		r.bgJobs.notifyJobDone(job)
	}()

	return formatBackgroundJobAck(job), nil
}

// 后台 job 对模型/用户的统一说明（超时降级与内部 StartBackground ACK 共用）。
const (
	backgroundJobAutoResultHint   = "完成后将自动回灌结果（async_tool_result），通常无需轮询 background_job_status；"
	backgroundJobOptionalMgmtHint = "若需取消或主动确认进度，可使用 background_job_cancel / background_job_status。"
)

func formatBackgroundJobAck(job *backgroundJob) string {
	return strings.Join([]string{
		fmt.Sprintf("[TOOL_BACKGROUND] tool_name=%s job_id=%s status=accepted", job.toolName, job.id),
		"任务已在后台执行。",
		backgroundJobAutoResultHint,
		backgroundJobOptionalMgmtHint,
	}, "\n")
}

func formatShellRunningResult(job *backgroundJob, params shellRunParams, reason string) string {
	st := params.shellType
	if job.bashShellType != "" {
		st = shellType(job.bashShellType)
	}
	reasonLine := "命令超过同步等待时间，已自动降级为后台任务。"
	if reason == "user" {
		reasonLine = "已按用户请求转为后台任务。"
	}
	return strings.Join([]string{
		fmt.Sprintf("[BASH_RESULT] status=RUNNING job_id=%s", job.id),
		fmt.Sprintf("shell_type=%s", st),
		reasonLine,
		backgroundJobAutoResultHint,
		backgroundJobOptionalMgmtHint,
	}, "\n")
}

func formatShellCancelledResult(job *backgroundJob, params shellRunParams) string {
	st := params.shellType
	if job != nil && job.bashShellType != "" {
		st = shellType(job.bashShellType)
	}
	return strings.Join([]string{
		"[BASH_RESULT] status=CANCELLED",
		fmt.Sprintf("shell_type=%s", st),
		"命令已被用户终止。",
	}, "\n")
}

func formatShellHardTimeoutResult(timeoutSec int) string {
	return strings.Join([]string{
		"[BASH_RESULT] status=ERROR",
		fmt.Sprintf("ERROR: 命令超过硬上限 %d 秒仍未结束，已终止（未转为后台）。", timeoutSec),
		"若需超时后自动转后台，请显式传入 timeout_seconds；也可在执行中通过 UI「转后台」。",
	}, "\n")
}

func (j *backgroundJob) statusText() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	lines := []string{
		fmt.Sprintf("[BACKGROUND_JOB_STATUS] job_id=%s tool_name=%s status=%s", j.id, j.toolName, j.status),
		fmt.Sprintf("started_at_unix_ms=%d", j.startedAt),
		fmt.Sprintf("finished_at_unix_ms=%d", j.finishedAt),
	}
	if j.bashCwd != "" {
		lines = append(lines, fmt.Sprintf("cwd=%q", j.bashCwd))
	}
	if j.bashTimeout > 0 {
		lines = append(lines, fmt.Sprintf("timeout_seconds=%d", j.bashTimeout))
	}
	if j.autoDegraded {
		lines = append(lines, "degraded_from_sync_timeout: true")
	}
	if j.status != "running" && j.result != "" {
		preview, truncated := clipText(j.result, 2000)
		lines = append(lines, "--- RESULT_PREVIEW ---", preview)
		if truncated {
			lines = append(lines, "[TRUNCATED] 预览已截断，完整结果在任务完成回灌消息中。")
		}
	}
	return strings.Join(lines, "\n")
}

func (j *backgroundJob) cancelJob() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.status == "running" {
		if j.bashCmd != nil && j.bashCmd.Process != nil && j.bashCmd.ProcessState == nil {
			killShellProcess(j.bashCmd)
		} else if j.cancelFn != nil {
			j.cancelFn()
		}
		j.status = "cancelled"
		j.finishedAt = nowMs()
		if j.result == "" {
			j.result = "任务已取消。"
		}
		return fmt.Sprintf("[BACKGROUND_JOB_CANCELLED] job_id=%s status=cancelled", j.id)
	}
	return fmt.Sprintf("[BACKGROUND_JOB_CANCELLED] job_id=%s status=%s", j.id, j.status)
}

func clipText(s string, limit int) (string, bool) {
	if len(s) <= limit {
		return s, false
	}
	return s[:limit], true
}
