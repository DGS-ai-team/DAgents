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

func backgroundJobStatusToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "background_job_status",
			Description: "查询 bash_run 后台任务状态与输出摘要。任务完成后通常已由 async_tool_result 自动回灌，无需轮询；仅在需取消或主动确认进度时使用。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"job_id": map[string]any{
						"type":        "string",
						"description": "bash_run 超时降级或历史后台任务返回的 job_id（必填）",
					},
				},
				"required":             []string{"job_id"},
				"additionalProperties": false,
			}),
		},
	}
}

func backgroundJobCancelToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "background_job_cancel",
			Description: "取消 bash_run 仍在运行的后台任务（含同步超时降级产生的 job）。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"job_id": map[string]any{
						"type":        "string",
						"description": "要取消的后台 job_id（必填）；须为 bash_run 超时降级产生且仍在运行",
					},
				},
				"required":             []string{"job_id"},
				"additionalProperties": false,
			}),
		},
	}
}

func (r *Registry) execBackgroundJobStatus(_ context.Context, raw json.RawMessage) (string, error) {
	_, cleaned := ParseRunInBackground(string(raw))
	var args backgroundJobIDArgs
	if err := json.Unmarshal([]byte(cleaned), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	job, ok := r.bgJobs.get(args.JobID)
	if !ok {
		return fmt.Sprintf("ERROR: 未找到后台任务：%q", args.JobID), nil
	}
	return job.statusText(), nil
}

func (r *Registry) execBackgroundJobCancel(_ context.Context, raw json.RawMessage) (string, error) {
	_, cleaned := ParseRunInBackground(string(raw))
	var args backgroundJobIDArgs
	if err := json.Unmarshal([]byte(cleaned), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	job, ok := r.bgJobs.get(args.JobID)
	if !ok {
		return fmt.Sprintf("ERROR: 未找到后台任务：%q", args.JobID), nil
	}
	msg := job.cancelJob()
	job.waitDone(5 * time.Second)
	// 超时降级的 collector 在 cancelled 时不会 notifyDone；工具取消也必须回灌。
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
