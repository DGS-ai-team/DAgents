package childagent

import "strings"

// 同进程临时 Agent（temporary agent）协议常量。
const (
	HitlScopeTemporaryAgent = "temporary_agent"

	EventTemporaryAgentCreated   = "temporary_agent_created"
	EventTemporaryAgentProgress  = "temporary_agent_progress"
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

// DefaultChildAllowedTools 在未指定 allowed_tools 时使用的默认工具集。
func DefaultChildAllowedTools() []string {
	return []string{"read_file", "glob_files", "grep_file", "bash_run"}
}

// FormatChildTask 规范化临时 Agent 首条 user 任务正文（角色与边界由子 Agent system prompt 承载）。
func FormatChildTask(task string) string {
	return strings.TrimSpace(task)
}

// ParentDelegatableTools 父 Agent 可下放给临时 Agent 的工具名（不含 skills 系列）。
func ParentDelegatableTools() []string {
	return []string{
		"read_file", "write_file", "glob_files", "grep_file", "grep_files", "search_replace", "bash_run",
		"background_job_status", "background_job_cancel",
	}
}

// IsParentOnlyTool 为仅父 Agent 可用的工具（含临时 Agent 管理工具、skills）。
func IsParentOnlyTool(name string) bool {
	switch strings.TrimSpace(name) {
	case ToolCreateTemporaryAgent, ToolWaitTemporaryAgents, ToolTemporaryAgentStatus, ToolCancelTemporaryAgent,
		ToolLoadSkills, ToolUnloadSkills, ToolClearSkills:
		return true
	default:
		return strings.HasPrefix(name, "trigger_") || name == "ask_user_information"
	}
}

// IsTemporaryAgentTool 判断是否为临时 Agent 管理工具（由 orchestrator 专用处理）。
func IsTemporaryAgentTool(name string) bool {
	switch strings.TrimSpace(name) {
	case ToolCreateTemporaryAgent, ToolWaitTemporaryAgents, ToolTemporaryAgentStatus, ToolCancelTemporaryAgent:
		return true
	default:
		return false
	}
}
