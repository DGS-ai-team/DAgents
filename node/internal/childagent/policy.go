package childagent

import "strings"

// 同进程临时 Agent（temporary agent）协议常量；与外部 A2A 工具/消息区分。
const (
	HitlScopeTemporaryAgent = "temporary_agent"

	EventTemporaryAgentCreated   = "temporary_agent_created"
	EventTemporaryAgentCompleted = "temporary_agent_completed"
	EventTemporaryAgentCancelled = "temporary_agent_cancelled"

	ToolCreateTemporaryAgent = "create_temporary_agent"
	ToolWaitTemporaryAgents  = "wait_temporary_agents"
	ToolTemporaryAgentStatus = "temporary_agent_status"
	ToolCancelTemporaryAgent = "cancel_temporary_agent"

	ToolLoadSkills   = "load_skills"
	ToolUnloadSkills = "unload_skills"
	ToolClearSkills  = "clear_skills"
)

// defaultChildSystemPrefix 为临时 Agent 首条 user 消息的系统前缀（由父 Agent 在 task 中提供角色与约束）。
const defaultChildSystemPrefix = "不要向用户追问。如果你只能完成部分任务，先完成部分任务，然后返回结果以及未完成任务的说明"

// DefaultChildAllowedTools 在未指定 allowed_tools 时使用的默认工具集。
func DefaultChildAllowedTools() []string {
	return []string{"read_file", "search_file", "bash_run"}
}

// FormatChildTask 将通用前缀与用户 task 合并为临时 Agent 首条 user 消息。
func FormatChildTask(task string) string {
	task = strings.TrimSpace(task)
	return strings.TrimSpace(defaultChildSystemPrefix + "\n\n任务：\n" + task)
}

// ParentDelegatableTools 父 Agent 可下放给临时 Agent 的工具名（不含 skills 系列）。
func ParentDelegatableTools() []string {
	return []string{
		"read_file", "write_file", "search_file", "search_replace", "bash_run",
		"background_job_status", "background_job_cancel",
	}
}

// IsParentOnlyTool 为仅父 Agent 可用的工具（含临时 Agent 管理工具、skills、非 A2A）。
func IsParentOnlyTool(name string) bool {
	switch strings.TrimSpace(name) {
	case ToolCreateTemporaryAgent, ToolWaitTemporaryAgents, ToolTemporaryAgentStatus, ToolCancelTemporaryAgent,
		ToolLoadSkills, ToolUnloadSkills, ToolClearSkills:
		return true
	default:
		return strings.HasPrefix(name, "trigger_") || name == "ask_user_information"
	}
}

// IsTemporaryAgentTool 判断是否为临时 Agent 管理工具（由 orchestrator 专用处理，非 A2A）。
func IsTemporaryAgentTool(name string) bool {
	switch strings.TrimSpace(name) {
	case ToolCreateTemporaryAgent, ToolWaitTemporaryAgents, ToolTemporaryAgentStatus, ToolCancelTemporaryAgent:
		return true
	default:
		return false
	}
}
