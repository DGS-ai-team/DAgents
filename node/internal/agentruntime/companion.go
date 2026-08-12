package agentruntime

import (
	"encoding/json"
	"strings"
)

// CompanionBrowserSuffix 伴生 browser Agent 的 id 后缀。
const CompanionBrowserSuffix = "-browser"

// CompanionMeta 写入父/伴生 config_snapshot 的伴生关系。
type CompanionMeta struct {
	// Role：父侧为空；伴生侧为 "browser"。
	Role string `json:"role,omitempty"`
	// OwnerAgentID：伴生指向所属主 Agent。
	OwnerAgentID string `json:"owner_agent_id,omitempty"`
	// BrowserAgentID：主 Agent 指向其 browser 伴生。
	BrowserAgentID string `json:"browser_agent_id,omitempty"`
}

// CompanionBrowserAgentID 由主 Agent id 派生伴生 id。
func CompanionBrowserAgentID(parentAgentID string) string {
	parentAgentID = strings.TrimSpace(parentAgentID)
	if parentAgentID == "" {
		return ""
	}
	if strings.HasSuffix(parentAgentID, CompanionBrowserSuffix) {
		return parentAgentID
	}
	return parentAgentID + CompanionBrowserSuffix
}

// IsCompanionBrowserAgentID 判断是否为派生的 browser 伴生 id。
func IsCompanionBrowserAgentID(agentID string) bool {
	return strings.HasSuffix(strings.TrimSpace(agentID), CompanionBrowserSuffix)
}

// SnapshotHasBrowserGroup 快照是否启用 browser 工具组。
func SnapshotHasBrowserGroup(snap Snapshot) bool {
	for _, g := range EnabledToolGroups(snap) {
		if strings.EqualFold(strings.TrimSpace(g), "browser") {
			return true
		}
	}
	return false
}

// CompanionFromSnapshot 读取 snapshot.companion。
func CompanionFromSnapshot(snap Snapshot) CompanionMeta {
	raw, ok := snap.Defaults["companion"]
	if !ok || raw == nil {
		// 也支持顶层 companion（与 defaults 并列写入 snapshot JSON）
		return CompanionMeta{}
	}
	return companionFromAny(raw)
}

// ParseCompanionMeta 从完整 snapshot JSON 解析 companion（顶层优先）。
func ParseCompanionMeta(raw json.RawMessage) CompanionMeta {
	if len(raw) == 0 {
		return CompanionMeta{}
	}
	var top struct {
		Companion map[string]any `json:"companion"`
		Defaults  map[string]any `json:"defaults"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		return CompanionMeta{}
	}
	if len(top.Companion) > 0 {
		return companionFromAny(top.Companion)
	}
	if top.Defaults != nil {
		if c, ok := top.Defaults["companion"]; ok {
			return companionFromAny(c)
		}
	}
	return CompanionMeta{}
}

func companionFromAny(v any) CompanionMeta {
	m, ok := v.(map[string]any)
	if !ok || m == nil {
		return CompanionMeta{}
	}
	out := CompanionMeta{}
	if s, ok := m["role"].(string); ok {
		out.Role = strings.TrimSpace(s)
	}
	if s, ok := m["owner_agent_id"].(string); ok {
		out.OwnerAgentID = strings.TrimSpace(s)
	}
	if s, ok := m["browser_agent_id"].(string); ok {
		out.BrowserAgentID = strings.TrimSpace(s)
	}
	return out
}

// WithCompanionMeta 将 companion 写入 snapshot JSON（顶层字段）。
func WithCompanionMeta(snapJSON json.RawMessage, meta CompanionMeta) (json.RawMessage, error) {
	var root map[string]any
	if len(snapJSON) == 0 {
		root = map[string]any{}
	} else if err := json.Unmarshal(snapJSON, &root); err != nil {
		return nil, err
	}
	c := map[string]any{}
	if meta.Role != "" {
		c["role"] = meta.Role
	}
	if meta.OwnerAgentID != "" {
		c["owner_agent_id"] = meta.OwnerAgentID
	}
	if meta.BrowserAgentID != "" {
		c["browser_agent_id"] = meta.BrowserAgentID
	}
	root["companion"] = c
	return json.Marshal(root)
}

// IsBrowserCompanionRecord 伴生角色标记。
func IsBrowserCompanionRecord(raw json.RawMessage) bool {
	meta := ParseCompanionMeta(raw)
	return strings.EqualFold(meta.Role, "browser") && meta.OwnerAgentID != ""
}
