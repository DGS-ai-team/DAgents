package hooks

import (
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/toolresult"
)

// ToolResultConfig 控制 tool.after_each 结果摘要与落盘（WS3）。
type ToolResultConfig struct {
	Enabled          bool
	MaxHistoryTokens int
	SpillSubdir      string
	Tools            []string
	FSRoot           string
}

// DefaultToolResultConfig 返回默认配置（bash_run，12k tokens 粗算）。
func DefaultToolResultConfig(fsRoot string) ToolResultConfig {
	c := toolresult.DefaultConfig(fsRoot)
	return ToolResultConfig{
		Enabled:          c.Enabled,
		MaxHistoryTokens: c.MaxHistoryTokens,
		SpillSubdir:      c.SpillSubdir,
		Tools:            append([]string(nil), c.Tools...),
		FSRoot:           c.FSRoot,
	}
}

func toolResultConfigUnset(c ToolResultConfig) bool {
	return c.MaxHistoryTokens == 0 &&
		strings.TrimSpace(c.SpillSubdir) == "" &&
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
	if c.MaxHistoryTokens > 0 {
		out.MaxHistoryTokens = c.MaxHistoryTokens
	}
	if strings.TrimSpace(c.SpillSubdir) != "" {
		out.SpillSubdir = strings.TrimSpace(c.SpillSubdir)
	}
	if len(c.Tools) > 0 {
		out.Tools = append([]string(nil), c.Tools...)
	}
	return out
}

func (c ToolResultConfig) toToolresultConfig() toolresult.Config {
	return toolresult.Config{
		Enabled:          c.Enabled,
		MaxHistoryTokens: c.MaxHistoryTokens,
		SpillSubdir:      c.SpillSubdir,
		Tools:            append([]string(nil), c.Tools...),
		FSRoot:           c.FSRoot,
	}.Normalized()
}

// RuntimeConfig 为 Orchestrator 注入的 Hook 运行时配置。
type RuntimeConfig struct {
	Duplicate  DuplicateConfig
	ToolResult ToolResultConfig
}

// RuntimeConfigOrDefault 合并 duplicate 与 tool result 默认值。
func RuntimeConfigOrDefault(c RuntimeConfig) RuntimeConfig {
	out := c
	out.Duplicate = DuplicateConfigOrDefault(c.Duplicate)
	out.ToolResult = ToolResultConfigOrDefault(c.ToolResult)
	return out
}
