// Package agentruntime 根据 Agent 模板快照构造 per-agent 运行时参数（FSRoot / 工具组等）。
package agentruntime

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

// SandboxSpec 来自模板/实例快照的沙箱字段。
type SandboxSpec struct {
	Enabled           bool   `json:"enabled"`
	Backend           string `json:"backend"`
	WorkspaceSubdir   string `json:"workspace_subdir"`
	FSRootIsolation   bool   `json:"fs_root_isolation"`
	AllowBash         bool   `json:"allow_bash"`
	AllowNetworkTools bool   `json:"allow_network_tools"`
}

// Snapshot 为 agents.config_snapshot_json 的解析视图。
type Snapshot struct {
	TemplateID string         `json:"template_id"`
	Defaults   map[string]any `json:"defaults"`
	Sandbox    SandboxSpec    `json:"sandbox"`
}

// ParseSnapshot 解析 config_snapshot JSON。
func ParseSnapshot(raw json.RawMessage) (Snapshot, error) {
	var snap Snapshot
	if len(raw) == 0 {
		return snap, nil
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		return Snapshot{}, fmt.Errorf("parse agent config snapshot: %w", err)
	}
	if strings.TrimSpace(snap.Sandbox.Backend) == "" {
		snap.Sandbox.Backend = "process"
	}
	if strings.TrimSpace(snap.Sandbox.WorkspaceSubdir) == "" {
		snap.Sandbox.WorkspaceSubdir = "data"
	}
	return snap, nil
}

// EffectiveFSRoot 返回该 Agent 的工具工作区根。
// 沙箱且 fs_root_isolation 时：<node_fs_root>/agents/<agent_id>/<workspace_subdir>
// 否则：Node 全局 fs_root。
func EffectiveFSRoot(nodeFSRoot, agentID string, snap Snapshot) string {
	nodeFSRoot = strings.TrimSpace(nodeFSRoot)
	agentID = strings.TrimSpace(agentID)
	if !snap.Sandbox.Enabled || !snap.Sandbox.FSRootIsolation || agentID == "" {
		return nodeFSRoot
	}
	sub := strings.TrimSpace(snap.Sandbox.WorkspaceSubdir)
	if sub == "" {
		sub = "data"
	}
	return filepath.Join(nodeFSRoot, "agents", agentID, sub)
}

// EnabledToolGroups 从 defaults.tools.enabled_groups 读取；无则 nil（表示沿用 Node 默认）。
func EnabledToolGroups(snap Snapshot) []string {
	toolsRaw, ok := snap.Defaults["tools"]
	if !ok || toolsRaw == nil {
		return nil
	}
	m, ok := toolsRaw.(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := m["enabled_groups"]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				continue
			}
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return append([]string(nil), v...)
	default:
		return nil
	}
}

// ApplySandboxToolConstraints 按沙箱约束收紧工具组。
// - allow_bash=false：去掉 bash
// - allow_network_tools=false：去掉 browser / a2a
func ApplySandboxToolConstraints(groups []string, snap Snapshot) []string {
	if !snap.Sandbox.Enabled {
		return groups
	}
	deny := map[string]struct{}{}
	if !snap.Sandbox.AllowBash {
		deny["bash"] = struct{}{}
	}
	if !snap.Sandbox.AllowNetworkTools {
		deny["browser"] = struct{}{}
		deny["a2a"] = struct{}{}
	}
	if len(deny) == 0 {
		return groups
	}
	// groups==nil 表示「全部」；需展开为全集再过滤。
	src := groups
	if len(src) == 0 {
		src = config.AllBuiltinToolGroupNames()
	}
	out := make([]string, 0, len(src))
	for _, g := range src {
		if _, blocked := deny[strings.ToLower(strings.TrimSpace(g))]; blocked {
			continue
		}
		out = append(out, g)
	}
	return out
}

// MultimodalEnabledFromDefaults 读取 defaults.llm.profiles[active].multimodal_enabled 或顶层 multimodal。
func MultimodalEnabledFromDefaults(snap Snapshot) *bool {
	llmRaw, ok := snap.Defaults["llm"]
	if !ok || llmRaw == nil {
		return nil
	}
	llmMap, ok := llmRaw.(map[string]any)
	if !ok {
		return nil
	}
	active, _ := llmMap["active"].(string)
	profiles, _ := llmMap["profiles"].(map[string]any)
	if active != "" && profiles != nil {
		if p, ok := profiles[active].(map[string]any); ok {
			if v, ok := p["multimodal_enabled"].(bool); ok {
				b := v
				return &b
			}
		}
	}
	return nil
}

// LLMActiveFromDefaults 读取 defaults.llm.active（Agent 绑定的 Node LLM 配置 id）。
func LLMActiveFromDefaults(snap Snapshot) string {
	llmRaw, ok := snap.Defaults["llm"]
	if !ok || llmRaw == nil {
		return ""
	}
	llmMap, ok := llmRaw.(map[string]any)
	if !ok {
		return ""
	}
	active, _ := llmMap["active"].(string)
	return strings.TrimSpace(active)
}

// MaxToolLoopsFromDefaults 读取 defaults.llm.max_tool_loops；无则返回 0。
func MaxToolLoopsFromDefaults(snap Snapshot) int {
	llmRaw, ok := snap.Defaults["llm"]
	if !ok || llmRaw == nil {
		return 0
	}
	llmMap, ok := llmRaw.(map[string]any)
	if !ok {
		return 0
	}
	switch v := llmMap["max_tool_loops"].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	default:
		return 0
	}
}

// ApplyDefaultsToTurnOptions 将快照 defaults 中的 llm / hooks 覆盖写入 TurnOptions。
func ApplyDefaultsToTurnOptions(turn *session.TurnOptions, snap Snapshot) {
	if turn == nil {
		return
	}
	if n := MaxToolLoopsFromDefaults(snap); n > 0 {
		turn.MaxToolLoops = n
	}
	hooksRaw, ok := snap.Defaults["hooks"]
	if !ok || hooksRaw == nil {
		return
	}
	m, ok := hooksRaw.(map[string]any)
	if !ok {
		return
	}
	if v, ok := boolPtrFromAny(m["inject_today_date_enabled"]); ok {
		turn.InjectTodayDate.Enabled = v
	}
	if v, ok := boolFromAny(m["tool_result_enabled"]); ok {
		turn.ToolResult.Enabled = v
	}
	if v, ok := intFromAny(m["tool_result_spill_threshold_tokens"]); ok && v > 0 {
		turn.ToolResult.SpillThresholdTokens = v
	}
	if v, ok := boolFromAny(m["duplicate_tool_call_enabled"]); ok {
		turn.DuplicateToolCall.Enabled = v
	}
	if v, ok := intFromAny(m["duplicate_tool_call_window_seconds"]); ok && v > 0 {
		turn.DuplicateToolCall.WindowSeconds = v
	}
	_ = hooks.InjectTodayDateConfigOrDefault(turn.InjectTodayDate)
}

func boolPtrFromAny(v any) (*bool, bool) {
	if v == nil {
		return nil, false
	}
	switch b := v.(type) {
	case bool:
		out := b
		return &out, true
	default:
		return nil, false
	}
}

func boolFromAny(v any) (bool, bool) {
	if v == nil {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

func intFromAny(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	default:
		return 0, false
	}
}
