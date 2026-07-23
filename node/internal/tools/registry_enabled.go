package tools

import "strings"

// SetMultimodalEnabled 控制 read_image、browser 视觉模式与 vision 载荷暂存；默认 false。
func (r *Registry) SetMultimodalEnabled(enabled bool) {
	if r == nil {
		return
	}
	r.multimodalEnabled = enabled
	r.registerBrowserTools()
}

// MultimodalEnabled 是否已启用多模态工具能力。
func (r *Registry) MultimodalEnabled() bool {
	return r != nil && r.multimodalEnabled
}

// SetBuiltinEnabled 设置 LLM 可见/可调用的内置工具允许列表；names 为空表示全部启用。
func (r *Registry) SetBuiltinEnabled(names []string) error {
	if r == nil {
		return nil
	}
	if len(names) == 0 {
		r.enabledOnly = nil
		return nil
	}
	set := make(map[string]struct{}, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if !IsKnownBuiltinTool(name) {
			return unknownBuiltinToolError(name)
		}
		set[name] = struct{}{}
	}
	if len(set) == 0 {
		r.enabledOnly = nil
		return nil
	}
	r.enabledOnly = set
	return nil
}

// IsKnownBuiltinTool 判断是否为可配置的内置工具名。
func IsKnownBuiltinTool(name string) bool {
	_, ok := knownBuiltinTools[name]
	return ok
}

func (r *Registry) toolEnabled(name string) bool {
	if r == nil || r.enabledOnly == nil {
		return true
	}
	_, ok := r.enabledOnly[name]
	return ok
}

func (r *Registry) filterToolDefs(defs []ToolDef) []ToolDef {
	if r == nil || r.enabledOnly == nil {
		return defs
	}
	out := make([]ToolDef, 0, len(defs))
	for _, def := range defs {
		if r.toolEnabled(def.Function.Name) {
			out = append(out, def)
		}
	}
	return out
}

// knownBuiltinTools 与 shared/config/builtin_tools.go 保持一致。
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
	"background_job_status":  {},
	"background_job_cancel":  {},
	"ask_user_information":   {},
	"remember":                 {},
	"load_skills":            {},
	"unload_skills":          {},
	"clear_skills":           {},
	"trigger_list":           {},
	"trigger_get":            {},
	"trigger_create":         {},
	"trigger_update":         {},
	"trigger_delete":         {},
	"agent_invoke":           {},
	"agent_discover":         {},
	"create_temporary_agent": {},
	"wait_temporary_agents":  {},
	"temporary_agent_status": {},
	"cancel_temporary_agent": {},
	"browser_start":          {},
	"browser_stop":           {},
	"browser_navigate":       {},
	"browser_click":          {},
	"browser_click_coordinate": {},
	"browser_fill":           {},
	"browser_press":          {},
	"browser_screenshot":     {},
	"browser_wait":           {},
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
	"wecom_send_markdown":      {},
	"wecom_send_file":          {},
}

func unknownBuiltinToolError(name string) error {
	return &unknownBuiltinTool{name: name}
}

type unknownBuiltinTool struct {
	name string
}

func (e *unknownBuiltinTool) Error() string {
	return "unknown builtin tool: " + e.name
}
