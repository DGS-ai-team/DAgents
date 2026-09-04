package config

import (
	"fmt"
	"sort"
	"strings"
)

// knownBuiltinTools 为 Node 可配置启用的内置工具名全集（与 node/internal/tools/registry.go 对齐）。
var knownBuiltinTools = map[string]struct{}{
	"read_file":              {},
	"show_image":             {},
	"read_image":             {},
	"write_file":             {},
	"glob_files":             {},
	"grep_file":              {},
	"grep_files":             {},
	"search_replace":         {},
	"bash_run":               {},
	"screen_capture":         {},
	"computer_use":           {},
	"terminal_config_list":   {},
	"terminal_open":          {},
	"terminal_input":         {},
	"terminal_read":          {},
	"terminal_terminate":     {},
	"terminal_list":          {},
	"terminal_command":       {},
	"terminal_upload":        {},
	"terminal_download":      {},
	"ask_user_information":   {},
	"remember":               {},
	"memory_search":          {},
	"memory_get":             {},
	"memory_forget":          {},
	"load_skills":            {},
	"unload_skills":          {},
	"clear_skills":           {},
	"trigger_list":           {},
	"trigger_get":            {},
	"trigger_create":         {},
	"trigger_update":         {},
	"trigger_delete":         {},
	"create_temporary_agent": {},
	"cancel_temporary_agent": {},
	"browser_run_task":       {},
	"browser_task_status":    {},
	"browser_task_cancel":    {},
	"wecom_send_markdown":    {},
	"wecom_send_file":        {},
}

// builtinToolGroups 为 Agent defaults.tools.enabled_groups 可配置的成组工具；组内工具须一并启用或禁用。
var builtinToolGroups = map[string][]string{
	"fs": {
		"read_file",
		"show_image",
		"read_image",
		"write_file",
		"glob_files",
		"grep_file",
		"grep_files",
		"search_replace",
	},
	"bash": {
		"bash_run",
	},
	"computer": {
		"screen_capture",
		"computer_use",
	},
	"terminal": {
		"terminal_config_list",
		"terminal_open",
		"terminal_input",
		"terminal_read",
		"terminal_terminate",
		"terminal_list",
		"terminal_command",
		"terminal_upload",
		"terminal_download",
	},
	"hitl": {
		"ask_user_information",
	},
	"memory": {
		"remember",
		"memory_search",
		"memory_get",
		"memory_forget",
	},
	"skills": {
		"load_skills",
		"unload_skills",
		"clear_skills",
	},
	"triggers": {
		"trigger_list",
		"trigger_get",
		"trigger_create",
		"trigger_update",
		"trigger_delete",
	},
	"child_agents": {
		"create_temporary_agent",
		"cancel_temporary_agent",
	},
	// browser：主 Agent 任务级派发（伴生 Chrome + sidecar browser_use.Agent）。
	// 细粒度 CDP/DOM 工具已退役，不再作为 LLM 工具暴露。
	"browser": {
		"browser_run_task",
		"browser_task_status",
		"browser_task_cancel",
	},
	"wecom": {
		"wecom_send_markdown",
		"wecom_send_file",
	},
}

var builtinToolToGroup map[string]string

func init() {
	builtinToolToGroup = make(map[string]string, len(knownBuiltinTools))
	for group, tools := range builtinToolGroups {
		for _, name := range tools {
			builtinToolToGroup[name] = group
		}
	}
	for name := range knownBuiltinTools {
		if _, ok := builtinToolToGroup[name]; !ok {
			panic("config: tool " + name + " missing from builtinToolGroups")
		}
	}
}

// AllBuiltinToolNames 返回已知内置工具名（字典序）。
func AllBuiltinToolNames() []string {
	out := make([]string, 0, len(knownBuiltinTools))
	for name := range knownBuiltinTools {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// AllBuiltinToolGroupNames 返回已知工具组名（字典序）。
func AllBuiltinToolGroupNames() []string {
	out := make([]string, 0, len(builtinToolGroups))
	for name := range builtinToolGroups {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// PublicBuiltinToolGroupNames 返回面向用户配置的工具组。
func PublicBuiltinToolGroupNames() []string {
	return AllBuiltinToolGroupNames()
}

// BuiltinToolGroupMembers 返回组内工具名（副本）；未知组返回 false。
func BuiltinToolGroupMembers(group string) ([]string, bool) {
	members, ok := builtinToolGroups[canonicalBuiltinToolGroup(group)]
	if !ok {
		return nil, false
	}
	out := make([]string, len(members))
	copy(out, members)
	return out, true
}

// NormalizeBuiltinToolGroups 去重并规范化工具组名；空切片表示未选任何组。
func NormalizeBuiltinToolGroups(groups []string) []string {
	if len(groups) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(groups))
	out := make([]string, 0, len(groups))
	for _, raw := range groups {
		name := canonicalBuiltinToolGroup(raw)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func canonicalBuiltinToolGroup(raw string) string {
	return strings.TrimSpace(raw)
}

// ExpandBuiltinToolGroups 将工具组展开为工具名列表；空切片表示未选任何组。
func ExpandBuiltinToolGroups(groups []string) []string {
	normalized := NormalizeBuiltinToolGroups(groups)
	if len(normalized) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	out := make([]string, 0, len(knownBuiltinTools))
	for _, group := range normalized {
		for _, tool := range builtinToolGroups[group] {
			if _, ok := seen[tool]; ok {
				continue
			}
			seen[tool] = struct{}{}
			out = append(out, tool)
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

// ValidateBuiltinToolGroups 校验工具组名已知且可展开为已知工具。
func ValidateBuiltinToolGroups(groups []string) error {
	for _, group := range NormalizeBuiltinToolGroups(groups) {
		if _, ok := builtinToolGroups[group]; !ok {
			return fmt.Errorf("unknown tool group %q", group)
		}
	}
	return validateBuiltinToolNames(ExpandBuiltinToolGroups(groups))
}

func validateBuiltinToolNames(names []string) error {
	for _, name := range names {
		if _, ok := knownBuiltinTools[name]; !ok {
			return fmt.Errorf("unknown builtin tool %q", name)
		}
	}
	return nil
}
