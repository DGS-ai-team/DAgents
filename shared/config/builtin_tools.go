package config

import (
	"fmt"
	"sort"
	"strings"
)

// knownBuiltinTools 为 Node 可配置启用的内置工具名全集（与 node/internal/tools/registry.go 对齐）。
var knownBuiltinTools = map[string]struct{}{
	"read_file":                {},
	"show_image":               {},
	"read_image":               {},
	"write_file":               {},
	"glob_files":               {},
	"grep_file":                {},
	"grep_files":               {},
	"search_replace":           {},
	"bash_run":                 {},
	"background_job_status":    {},
	"background_job_cancel":    {},
	"ask_user_information":     {},
	"remember":                 {},
	"load_skills":              {},
	"unload_skills":            {},
	"clear_skills":             {},
	"trigger_list":             {},
	"trigger_get":              {},
	"trigger_create":           {},
	"trigger_update":           {},
	"trigger_delete":           {},
	"agent_invoke":             {},
	"agent_discover":           {},
	"create_temporary_agent":   {},
	"wait_temporary_agents":    {},
	"temporary_agent_status":   {},
	"cancel_temporary_agent":   {},
	"browser_start":            {},
	"browser_stop":             {},
	"browser_navigate":         {},
	"browser_click":            {},
	"browser_click_coordinate": {},
	"browser_fill":             {},
	"browser_press":            {},
	"browser_screenshot":       {},
	"browser_wait":             {},
	"browser_snapshot":         {},
	"browser_search":           {},
	"browser_go_back":          {},
	"browser_scroll":           {},
	"browser_find_text":        {},
	"browser_switch_tab":       {},
	"browser_close_tab":        {},
	"browser_extract":          {},
	"browser_evaluate":         {},
	"browser_find_elements":    {},
	"browser_search_page":      {},
	"browser_upload_file":      {},
	"browser_dropdown_options": {},
	"browser_select_dropdown":  {},
}

// builtinToolGroups 为 tools.enabled_groups 可配置的成组工具；组内工具须一并启用或禁用。
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
		"background_job_status",
		"background_job_cancel",
	},
	"hitl": {
		"ask_user_information",
		"remember",
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
	"a2a": {
		"agent_invoke",
		"agent_discover",
	},
	"child_agents": {
		"create_temporary_agent",
		"wait_temporary_agents",
		"temporary_agent_status",
		"cancel_temporary_agent",
	},
	"browser": {
		"browser_start",
		"browser_stop",
		"browser_navigate",
		"browser_click",
		"browser_click_coordinate",
		"browser_fill",
		"browser_press",
		"browser_screenshot",
		"browser_wait",
		"browser_snapshot",
		"browser_search",
		"browser_go_back",
		"browser_scroll",
		"browser_find_text",
		"browser_switch_tab",
		"browser_close_tab",
		"browser_extract",
		"browser_evaluate",
		"browser_find_elements",
		"browser_search_page",
		"browser_upload_file",
		"browser_dropdown_options",
		"browser_select_dropdown",
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

// BuiltinToolGroupMembers 返回组内工具名（副本）；未知组返回 false。
func BuiltinToolGroupMembers(group string) ([]string, bool) {
	members, ok := builtinToolGroups[group]
	if !ok {
		return nil, false
	}
	out := make([]string, len(members))
	copy(out, members)
	return out, true
}

// NormalizedBuiltinEnabledGroups 去重并规范化 tools.enabled_groups；空切片表示「未配置允许列表」。
func (t *ToolsConfig) NormalizedBuiltinEnabledGroups() []string {
	if t == nil || len(t.EnabledGroups) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(t.EnabledGroups))
	out := make([]string, 0, len(t.EnabledGroups))
	for _, raw := range t.EnabledGroups {
		name := strings.TrimSpace(raw)
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

// NormalizedBuiltinEnabled 将 tools.enabled_groups 展开为工具名列表；空切片表示「未配置允许列表」。
func (t *ToolsConfig) NormalizedBuiltinEnabled() []string {
	groups := t.NormalizedBuiltinEnabledGroups()
	if len(groups) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	out := make([]string, 0, len(knownBuiltinTools))
	for _, group := range groups {
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

func validateToolsEnabledConfig(t *ToolsConfig) error {
	if t == nil {
		return nil
	}
	if len(t.Enabled) > 0 {
		return fmt.Errorf("tools.enabled is deprecated; use tools.enabled_groups (groups: %s)", strings.Join(AllBuiltinToolGroupNames(), ", "))
	}
	for _, group := range t.NormalizedBuiltinEnabledGroups() {
		if _, ok := builtinToolGroups[group]; !ok {
			return fmt.Errorf("tools.enabled_groups contains unknown group %q", group)
		}
	}
	return validateBuiltinToolNames(t.NormalizedBuiltinEnabled())
}

func validateBuiltinToolNames(names []string) error {
	for _, name := range names {
		if _, ok := knownBuiltinTools[name]; !ok {
			return fmt.Errorf("tools.enabled_groups expands to unknown tool %q", name)
		}
	}
	return nil
}
