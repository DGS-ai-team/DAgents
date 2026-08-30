package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type backgroundJobIDArgs struct {
	JobID string `json:"job_id"`
}

func (r *Registry) execBackgroundJobStatus(ctx context.Context, raw json.RawMessage) (string, error) {
	_, cleaned := ParseToolCallArguments(string(raw))
	var args backgroundJobIDArgs
	if err := json.Unmarshal([]byte(cleaned), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	job, ok := r.bgJobs.get(args.JobID)
	if !ok {
		return fmt.Sprintf("ERROR: 未找到后台任务：%q", args.JobID), nil
	}
	status := job.statusText()
	job.mu.Lock()
	jobStatus := job.status
	var recovery RemoteProcessRecovery
	if job.remoteRecovery != nil {
		recovery = *job.remoteRecovery
	}
	job.mu.Unlock()
	if jobStatus == jobStatusUnknown && recovery.JobToken != "" && r.linuxProvider != nil {
		remoteStatus, err := r.linuxProvider.InspectRemoteProcess(ctx, r.agentID, recovery)
		if err != nil {
			status += "\nremote_status: unavailable"
		} else {
			status += "\nremote_status: " + remoteStatus
		}
	}
	return status, nil
}

func (r *Registry) execBackgroundJobCancel(_ context.Context, raw json.RawMessage) (string, error) {
	_, cleaned := ParseToolCallArguments(string(raw))
	var args backgroundJobIDArgs
	if err := json.Unmarshal([]byte(cleaned), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	job, ok := r.bgJobs.get(args.JobID)
	if !ok {
		return fmt.Sprintf("ERROR: 未找到后台任务：%q", args.JobID), nil
	}
	if message, handled, err := r.cancelRecoveredBackgroundJob(context.Background(), job); handled {
		if err != nil {
			return "", err
		}
		return message, nil
	}
	msg := job.cancelJob()
	job.waitDone(5 * time.Second)
	// 取消后的后台任务也必须回灌一次终态通知；notifyJobDone 负责幂等。
	if r.bgJobs != nil {
		r.bgJobs.notifyJobDone(job)
	}
	return msg, nil
}

// IsBackgroundJobTool 判断是否为后台任务管理工具（始终同步执行）。
func IsBackgroundJobTool(name string) bool {
	switch name {
	case "background_job_status", "background_job_cancel":
		return true
	default:
		return false
	}
}
