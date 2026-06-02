package childagent

import (
	"fmt"
	"strings"
)

// Template 为内置子 Agent 模板。
type Template struct {
	ID            string
	Description   string
	SystemPrefix  string
	DefaultTools  []string
	DefaultMaxTurns int
}

var templates = map[string]Template{
	"general-helper": {
		ID:              "general-helper",
		Description:     "通用子任务",
		SystemPrefix:    "你是通用任务助手，在限定工具内完成父 Agent 委派的自包含任务；不要向用户追问。",
		DefaultTools:    []string{"read_file", "search_file", "bash_run"},
		DefaultMaxTurns: 20,
	},
	"code-review-helper": {
		ID:              "code-review-helper",
		Description:     "只读代码审查",
		SystemPrefix:    "你是代码审查助手，只读分析代码，列出风险与建议，不要修改文件。",
		DefaultTools:    []string{"read_file", "search_file"},
		DefaultMaxTurns: 15,
	},
	"research-helper": {
		ID:              "research-helper",
		Description:     "信息搜集",
		SystemPrefix:    "你是调研助手，收集并归纳信息，输出结构化结论。",
		DefaultTools:    []string{"read_file", "search_file", "bash_run"},
		DefaultMaxTurns: 25,
	},
	"ops-check-helper": {
		ID:              "ops-check-helper",
		Description:     "运维巡检",
		SystemPrefix:    "你是运维巡检助手，执行只读或低风险检查并汇总状态。",
		DefaultTools:    []string{"bash_run", "read_file"},
		DefaultMaxTurns: 20,
	},
}

// LookupTemplate 返回模板；未知 id 返回 error。
func LookupTemplate(id string) (Template, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		id = "general-helper"
	}
	t, ok := templates[id]
	if !ok {
		return Template{}, fmt.Errorf("unknown template_id %q", id)
	}
	return t, nil
}

// FormatTask 将模板前缀与用户 task 合并为子 Agent 首条 user 消息。
func FormatTask(t Template, task string) string {
	task = strings.TrimSpace(task)
	return strings.TrimSpace(t.SystemPrefix + "\n\n任务：\n" + task)
}

// ParentDelegatableTools 父 Agent 可下放给子 Agent 的工具名。
func ParentDelegatableTools() []string {
	return []string{
		"read_file", "write_file", "search_file", "search_replace", "bash_run",
		"load_skills", "unload_skills", "clear_skills",
		"background_job_status", "background_job_cancel",
	}
}

// IsParentOnlyTool 为仅父 Agent 可用的工具（含子 Agent 管理工具）。
func IsParentOnlyTool(name string) bool {
	switch strings.TrimSpace(name) {
	case "create_temporary_agent", "wait_child_agents", "child_agent_status", "cancel_child_agent":
		return true
	default:
		return strings.HasPrefix(name, "trigger_") || name == "ask_user_information"
	}
}

// IsChildAgentTool 判断是否子 Agent 管理工具（由 orchestrator 专用处理）。
func IsChildAgentTool(name string) bool {
	switch strings.TrimSpace(name) {
	case "create_temporary_agent", "wait_child_agents", "child_agent_status", "cancel_child_agent":
		return true
	default:
		return false
	}
}
