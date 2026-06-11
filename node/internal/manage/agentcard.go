package manage

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// AgentCard 为注册时上报 Manage 的 Agent 名片（JSON 文件）。
type AgentCard struct {
	Name               string         `json:"name"`
	Description        string         `json:"description"`
	URL                string         `json:"url"`
	Version            string         `json:"version"`
	Capabilities       []string       `json:"capabilities"`
	Skills             []string       `json:"skills"`
	DefaultInputModes  []string       `json:"defaultInputModes"`
	DefaultOutputModes []string       `json:"defaultOutputModes"`
	Metadata           map[string]any `json:"metadata"`
}

// LoadAgentCard 从 JSON 文件加载 Agent Card；路径为空或文件不存在返回 nil, nil。
func LoadAgentCard(path string) (*AgentCard, error) {
	cleaned := strings.TrimSpace(path)
	if cleaned == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(cleaned)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read agent card %q: %w", cleaned, err)
	}
	var card AgentCard
	if err := json.Unmarshal(raw, &card); err != nil {
		return nil, fmt.Errorf("parse agent card %q: %w", cleaned, err)
	}
	return &card, nil
}

func (c *AgentCard) role() string {
	if c == nil || c.Metadata == nil {
		return ""
	}
	if v, ok := c.Metadata["role"].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// CompliancePeer 返回 Agent Card metadata.compliance_peer（A2A 默认合规/协作对端）。
func (c *AgentCard) CompliancePeer() string {
	if c == nil || c.Metadata == nil {
		return ""
	}
	if v, ok := c.Metadata["compliance_peer"].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func (c *AgentCard) asMap() map[string]any {
	if c == nil {
		return nil
	}
	out := map[string]any{
		"name":        strings.TrimSpace(c.Name),
		"description": strings.TrimSpace(c.Description),
	}
	if c.URL != "" {
		out["url"] = c.URL
	}
	if c.Version != "" {
		out["version"] = c.Version
	}
	if len(c.Capabilities) > 0 {
		out["capabilities"] = append([]string(nil), c.Capabilities...)
	}
	if len(c.Skills) > 0 {
		out["skills"] = append([]string(nil), c.Skills...)
	}
	if len(c.DefaultInputModes) > 0 {
		out["defaultInputModes"] = append([]string(nil), c.DefaultInputModes...)
	}
	if len(c.DefaultOutputModes) > 0 {
		out["defaultOutputModes"] = append([]string(nil), c.DefaultOutputModes...)
	}
	if len(c.Metadata) > 0 {
		out["metadata"] = c.Metadata
	}
	return out
}
