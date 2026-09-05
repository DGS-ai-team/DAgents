package hooks

import (
	"log/slog"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/toolresult"
)

// ToolResultConfig 控制 tool.after_each 结果摘要与落盘（WS3）。
type ToolResultConfig struct {
	// Enabled 为 nil 时表示未配置，使用默认值 true；非 nil 时尊重显式开关。
	Enabled              *bool
	SpillThresholdTokens int
	Tools                []string
	WorkspaceRoot        string
	AgentID              string
}

// DefaultToolResultConfig 返回默认配置（bash + terminal + fs 组，12k spill 阈值）。
func DefaultToolResultConfig(workspaceRoot string) ToolResultConfig {
	c := toolresult.DefaultConfig(workspaceRoot)
	enabled := c.Enabled
	return ToolResultConfig{
		Enabled:              &enabled,
		SpillThresholdTokens: c.SpillThresholdTokens,
		Tools:                append([]string(nil), c.Tools...),
		WorkspaceRoot:        c.WorkspaceRoot,
	}
}

func toolResultConfigUnset(c ToolResultConfig) bool {
	return c.SpillThresholdTokens == 0 &&
		len(c.Tools) == 0 &&
		strings.TrimSpace(c.WorkspaceRoot) == "" &&
		strings.TrimSpace(c.AgentID) == "" &&
		c.Enabled == nil
}

// ToolResultConfigOrDefault 合并默认值。
func ToolResultConfigOrDefault(c ToolResultConfig) ToolResultConfig {
	if toolResultConfigUnset(c) {
		return DefaultToolResultConfig("")
	}
	out := DefaultToolResultConfig(c.WorkspaceRoot)
	if c.WorkspaceRoot != "" {
		out.WorkspaceRoot = strings.TrimSpace(c.WorkspaceRoot)
	}
	if c.AgentID != "" {
		out.AgentID = strings.TrimSpace(c.AgentID)
	}
	if c.Enabled != nil {
		enabled := *c.Enabled
		out.Enabled = &enabled
	}
	if c.SpillThresholdTokens > 0 {
		out.SpillThresholdTokens = c.SpillThresholdTokens
	}
	if len(c.Tools) > 0 {
		out.Tools = append([]string(nil), c.Tools...)
	}
	return out
}

// IsEnabled 返回是否启用。nil 仅在调用方传入未归一化配置时出现，此时采用默认值。
func (c ToolResultConfig) IsEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}

func (c ToolResultConfig) toToolresultConfig() toolresult.Config {
	return toolresult.Config{
		Enabled:              c.IsEnabled(),
		SpillThresholdTokens: c.SpillThresholdTokens,
		Tools:                append([]string(nil), c.Tools...),
		WorkspaceRoot:        c.WorkspaceRoot,
		AgentID:              c.AgentID,
	}.Normalized()
}

// RuntimeConfig 为 Orchestrator 注入的 Hook 运行时配置。
type RuntimeConfig struct {
	Duplicate       DuplicateConfig
	ToolResult      ToolResultConfig
	AgentOwnedFile  AgentOwnedFileConfig
	InjectTodayDate InjectTodayDateConfig
	Plugins         PluginsConfig
	Logger          *slog.Logger
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
	out.InjectTodayDate = InjectTodayDateConfigOrDefault(c.InjectTodayDate)
	return out
}
