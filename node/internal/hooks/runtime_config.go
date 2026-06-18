package hooks

import (
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/toolresult"
)

// ToolResultConfig 控制 tool.after_each 结果摘要与落盘（WS3）。
type ToolResultConfig struct {
	Enabled              bool
	SpillThresholdTokens int
	Tools                []string
	FSRoot               string
}

// DefaultToolResultConfig 返回默认配置（bash + fs 组，12k spill 阈值）。
func DefaultToolResultConfig(fsRoot string) ToolResultConfig {
	c := toolresult.DefaultConfig(fsRoot)
	return ToolResultConfig{
		Enabled:              c.Enabled,
		SpillThresholdTokens: c.SpillThresholdTokens,
		Tools:                append([]string(nil), c.Tools...),
		FSRoot:               c.FSRoot,
	}
}

func toolResultConfigUnset(c ToolResultConfig) bool {
	return c.SpillThresholdTokens == 0 &&
		len(c.Tools) == 0 &&
		strings.TrimSpace(c.FSRoot) == "" &&
		!c.Enabled
}

// ToolResultConfigOrDefault 合并默认值。
func ToolResultConfigOrDefault(c ToolResultConfig) ToolResultConfig {
	if toolResultConfigUnset(c) {
		return DefaultToolResultConfig("")
	}
	out := DefaultToolResultConfig(c.FSRoot)
	if c.FSRoot != "" {
		out.FSRoot = strings.TrimSpace(c.FSRoot)
	}
	out.Enabled = c.Enabled
	if c.SpillThresholdTokens > 0 {
		out.SpillThresholdTokens = c.SpillThresholdTokens
	}
	if len(c.Tools) > 0 {
		out.Tools = append([]string(nil), c.Tools...)
	}
	return out
}

func (c ToolResultConfig) toToolresultConfig() toolresult.Config {
	return toolresult.Config{
		Enabled:              c.Enabled,
		SpillThresholdTokens: c.SpillThresholdTokens,
		Tools:                append([]string(nil), c.Tools...),
		FSRoot:               c.FSRoot,
	}.Normalized()
}

// RuntimeConfig 为 Orchestrator 注入的 Hook 运行时配置。
type RuntimeConfig struct {
	Duplicate      DuplicateConfig
	ToolResult     ToolResultConfig
	AgentOwnedFile AgentOwnedFileConfig
}

// AgentOwnedFileConfig 控制 Agent 自有文件写操作信任链。
type AgentOwnedFileConfig struct {
	Enabled    bool
	PathStater PathStater
}

// DefaultAgentOwnedFileConfig 返回默认配置（启用）。
func DefaultAgentOwnedFileConfig() AgentOwnedFileConfig {
	return AgentOwnedFileConfig{Enabled: true}
}

// AgentOwnedFileConfigOrDefault 合并默认值。
func AgentOwnedFileConfigOrDefault(c AgentOwnedFileConfig) AgentOwnedFileConfig {
	if c == (AgentOwnedFileConfig{}) {
		return DefaultAgentOwnedFileConfig()
	}
	out := DefaultAgentOwnedFileConfig()
	out.Enabled = c.Enabled
	if c.PathStater != nil {
		out.PathStater = c.PathStater
	}
	return out
}

// RuntimeConfigOrDefault 合并 duplicate、tool result 与 agent owned file 默认值。
func RuntimeConfigOrDefault(c RuntimeConfig) RuntimeConfig {
	out := c
	out.Duplicate = DuplicateConfigOrDefault(c.Duplicate)
	out.ToolResult = ToolResultConfigOrDefault(c.ToolResult)
	out.AgentOwnedFile = AgentOwnedFileConfigOrDefault(c.AgentOwnedFile)
	return out
}
