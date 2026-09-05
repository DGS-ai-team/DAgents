// Package agentruntime 根据 Agent 模板快照构造 per-agent 运行时参数（workspace / 工具组等）。
package agentruntime

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

// Snapshot 为 agents.config_snapshot_json 的解析视图。
type Snapshot struct {
	TemplateID string          `json:"template_id"`
	Defaults   map[string]any  `json:"defaults"`
	Workspace  WorkspaceConfig `json:"workspace,omitempty"`
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
	return snap, nil
}

// EnabledToolGroups 从 defaults.tools.enabled_groups 读取。
// 未配置或空列表表示不启用任何工具组（不再回退 Node tools.enabled_groups）。
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
		return config.NormalizeBuiltinToolGroups(out)
	case []string:
		return config.NormalizeBuiltinToolGroups(v)
	default:
		return nil
	}
}

// ToolsetShrinks 判断工具组缩水：旧组展开后的任一工具名不在新组展开结果中。
func ToolsetShrinks(oldGroups, newGroups []string) bool {
	oldNames := config.ExpandBuiltinToolGroups(oldGroups)
	if len(oldNames) == 0 {
		return false
	}
	newSet := make(map[string]struct{}, len(newGroups)*4)
	for _, name := range config.ExpandBuiltinToolGroups(newGroups) {
		newSet[name] = struct{}{}
	}
	for _, name := range oldNames {
		if _, ok := newSet[name]; !ok {
			return true
		}
	}
	return false
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
				return &v
			}
		}
	}
	return nil
}

// EffectiveMultimodalEnabled 解析 Agent 生效的多模态开关：
// 1) snapshot 内嵌 profiles[active].multimodal_enabled
// 2) 否则按 defaults.llm.active 查 Node LLM 档案
// 3) 再否则回退 Node 进程当前 multimodal
func EffectiveMultimodalEnabled(nodeCFG *config.Config, snap Snapshot) bool {
	if v := MultimodalEnabledFromDefaults(snap); v != nil {
		return *v
	}
	if nodeCFG != nil {
		if active := LLMActiveFromDefaults(snap); active != "" {
			if p, ok := nodeCFG.LLM.GetProfile(active); ok {
				return config.ProfileMultimodalEnabled(p)
			}
		}
		return nodeCFG.MultimodalEnabled()
	}
	return false
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

// MaxStepsFromDefaults 读取 defaults.llm.max_steps；无则返回 0。
func MaxStepsFromDefaults(snap Snapshot) int {
	llmRaw, ok := snap.Defaults["llm"]
	if !ok || llmRaw == nil {
		return 0
	}
	llmMap, ok := llmRaw.(map[string]any)
	if !ok {
		return 0
	}
	switch v := llmMap["max_steps"].(type) {
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

// SkillsConfig 为 defaults.skills 解析结果；能力开关见工具组 skills。
type SkillsConfig struct {
	// VisibleRestrict 表示 snapshot 显式写了 visible（含空列表）；false 表示未限制。
	VisibleRestrict bool
	Visible         []string
}

// SkillsFromDefaults 读取 defaults.skills.visible。
// visible 缺省：不限制（全部可见）；visible: []：全部不可见；visible: ["a"]：仅 a。
// 不再读取 defaults.skills.enabled（旧字段忽略；能力由工具组 skills 决定）。
func SkillsFromDefaults(snap Snapshot) SkillsConfig {
	var out SkillsConfig
	raw, ok := snap.Defaults["skills"]
	if !ok || raw == nil {
		return out
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return out
	}
	if _, hasVisible := m["visible"]; hasVisible {
		out.VisibleRestrict = true
		out.Visible = stringSliceFromAny(m["visible"])
	}
	return out
}

func stringSliceFromAny(v any) []string {
	switch items := v.(type) {
	case []any:
		out := make([]string, 0, len(items))
		seen := map[string]struct{}{}
		for _, item := range items {
			s, ok := item.(string)
			if !ok {
				continue
			}
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if _, dup := seen[s]; dup {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
		return out
	case []string:
		out := make([]string, 0, len(items))
		seen := map[string]struct{}{}
		for _, s := range items {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if _, dup := seen[s]; dup {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
		return out
	default:
		return nil
	}
}

// ApplyDefaultsToTurnOptions 将快照 defaults 中的 llm / hooks / prompt_context 写入 TurnOptions。
// max_steps 仅来自 Agent snapshot（新建时写入）；缺省时用 DefaultMaxSteps。
func ApplyDefaultsToTurnOptions(turn *session.TurnOptions, snap Snapshot) {
	if turn == nil {
		return
	}
	n := MaxStepsFromDefaults(snap)
	if n <= 0 {
		n = DefaultMaxSteps
	}
	turn.Budget.MaxSteps = n
	if pc := PromptContextFromDefaults(snap); pc != nil {
		turn.PromptContext = *pc
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
	if v, ok := boolPtrFromAny(m["tool_result_enabled"]); ok {
		turn.ToolResult.Enabled = v
	}
	if v, ok := intFromAny(m["tool_result_spill_threshold_tokens"]); ok && v > 0 {
		turn.ToolResult.SpillThresholdTokens = v
	}
	if v, ok := boolPtrFromAny(m["duplicate_tool_call_enabled"]); ok {
		turn.DuplicateToolCall.Enabled = v
	}
	if v, ok := intFromAny(m["duplicate_tool_call_window_seconds"]); ok && v > 0 {
		turn.DuplicateToolCall.WindowSeconds = v
	}
	_ = hooks.InjectTodayDateConfigOrDefault(turn.InjectTodayDate)
}

// PromptContextFromDefaults 读取 defaults.prompt_context 开关。
func PromptContextFromDefaults(snap Snapshot) *session.PromptContextOptions {
	raw, ok := snap.Defaults["prompt_context"]
	if !ok || raw == nil {
		return nil
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	out := &session.PromptContextOptions{}
	if v, ok := boolPtrFromAny(m["soul_enabled"]); ok {
		out.SoulEnabled = v
	}
	if v, ok := boolPtrFromAny(m["custom_enabled"]); ok {
		out.CustomEnabled = v
	}
	if v, ok := boolPtrFromAny(m["memory_enabled"]); ok {
		out.MemoryEnabled = v
	}
	if v, ok := stringFromAny(m["memory_scope"]); ok {
		scope := strings.TrimSpace(v)
		if scope == MemoryScopeGlobal || scope == MemoryScopeAgent {
			out.MemoryScope = &scope
		}
	}
	return out
}

const (
	MemoryScopeGlobal = "global"
	MemoryScopeAgent  = "agent"
)

// MemoryScopeFromDefaults 返回记忆作用域，默认 agent（独立）。
func MemoryScopeFromDefaults(snap Snapshot) string {
	pc := PromptContextFromDefaults(snap)
	if pc != nil && pc.MemoryScope != nil {
		scope := strings.TrimSpace(*pc.MemoryScope)
		if scope == MemoryScopeGlobal {
			return MemoryScopeGlobal
		}
	}
	return MemoryScopeAgent
}

func stringFromAny(v any) (string, bool) {
	if v == nil {
		return "", false
	}
	s, ok := v.(string)
	return strings.TrimSpace(s), ok && s != ""
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
