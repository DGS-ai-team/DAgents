package tools

import (
	"fmt"
	"strings"
)

// 后台 job 对模型/用户的统一说明（超时降级与 run_in_background ACK 共用）。
const (
	backgroundJobAutoResultHint = "完成后将自动回灌结果（async_tool_result）；通常无需轮询 background_job_status。"
	backgroundJobOptionalMgmtHint = "若需取消或主动查询进度，可使用 background_job_cancel / background_job_status。"
)

func formatBackgroundJobAck(job *backgroundJob) string {
	return strings.Join([]string{
		fmt.Sprintf("[TOOL_BACKGROUND] tool_name=%s job_id=%s status=accepted", job.toolName, job.id),
		"任务已在后台执行。",
		backgroundJobAutoResultHint,
		backgroundJobOptionalMgmtHint,
	}, "\n")
}

func formatShellRunningResult(job *backgroundJob, params shellRunParams) string {
	st := params.shellType
	if job.bashShellType != "" {
		st = shellType(job.bashShellType)
	}
	return strings.Join([]string{
		fmt.Sprintf("[BASH_RESULT] status=RUNNING job_id=%s", job.id),
		fmt.Sprintf("shell_type=%s", st),
		"命令超过同步等待时间，已自动降级为后台任务。",
		backgroundJobAutoResultHint,
		backgroundJobOptionalMgmtHint,
	}, "\n")
}
