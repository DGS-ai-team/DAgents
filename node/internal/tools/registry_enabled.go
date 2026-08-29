package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

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

// SetBuiltinEnabled 设置 LLM 可见/可执行的内置工具允许列表；names 为空表示全部启用。
// 未列入的工具仍保留 handler（供子 Agent bypass 委托），但 Execute/StartBackground 会 soft reject。
func (r *Registry) SetBuiltinEnabled(names []string) error {
	if r == nil {
		return nil
	}
	if len(names) == 0 {
		r.enabledOnly = nil
		r.legacyLinuxTools = false
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
	// Old Agent snapshots may still explicitly list the deprecated tools. Keep
	// their model-visible definitions for one compatibility window, while a
	// new snapshot using the terminal group receives only terminal_* tools.
	r.legacyLinuxTools = false
	for _, name := range []string{"linux_exec", "linux_file_upload", "linux_file_download"} {
		if _, ok := set[name]; ok {
			r.legacyLinuxTools = true
			break
		}
	}
	return nil
}

// ErrToolNotEnabled 返回未启用工具的 soft-reject 文案（对齐 Workgroup allowlist 风格）。
func ErrToolNotEnabled(name string) error {
	return fmt.Errorf("ERROR: tool %q is not enabled", strings.TrimSpace(name))
}

func (r *Registry) rejectIfDisabled(ctx context.Context, name string) error {
	if EnabledBypassFromContext(ctx) {
		return nil
	}
	if r.toolEnabled(name) {
		return nil
	}
	return ErrToolNotEnabled(name)
}

// SetBuiltinEnabledNone 禁用全部内置工具（显式空允许列表）。
func (r *Registry) SetBuiltinEnabledNone() {
	if r == nil {
		return
	}
	r.enabledOnly = map[string]struct{}{}
	r.legacyLinuxTools = false
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
	if _, ok := r.mcpTools[name]; ok {
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

// knownBuiltinTools 使用 shared/config 的唯一工具目录，避免 Node 和配置模块
// 分别维护一份容易漂移的工具名清单。
var knownBuiltinTools = func() map[string]struct{} {
	names := config.AllBuiltinToolNames()
	set := make(map[string]struct{}, len(names))
	for _, name := range names {
		set[name] = struct{}{}
	}
	return set
}()

func unknownBuiltinToolError(name string) error {
	return &unknownBuiltinTool{name: name}
}

type unknownBuiltinTool struct {
	name string
}

func (e *unknownBuiltinTool) Error() string {
	return "unknown builtin tool: " + e.name
}
