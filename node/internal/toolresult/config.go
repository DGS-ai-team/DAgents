package toolresult

import (
	"strings"

	sharedconfig "github.com/DGS-ai-team/DAgents/shared/config"
)

const (
	// DefaultSpillThresholdTokens 单条 tool 结果写入 history 前触发落盘摘要的 token 阈值（DeepSeek 粗算）。
	DefaultSpillThresholdTokens = 12000
	// spillSubdir 相对 Agent workspace 的固定落盘目录（不可配置）。
	spillSubdir = "tool_outputs"
)

// Config 控制 tool.after_each 结果摘要与落盘（WS3）。
type Config struct {
	Enabled bool
	// SpillThresholdTokens 单条 tool 结果超过该估算 token 数时落盘并对 history 做头尾摘要。
	SpillThresholdTokens int
	// Tools 为启用 spill/摘要 的工具名列表；空表示不处理任何工具。
	Tools         []string
	WorkspaceRoot string // Agent workspace 绝对路径，用于落盘
	AgentID       string // 用于共享 workspace 时隔离 Agent 结果文件
}

// DefaultConfig 返回 WS3 默认（bash + terminal + fs）。
func DefaultConfig(workspaceRoot string) Config {
	return Config{
		Enabled:              true,
		SpillThresholdTokens: DefaultSpillThresholdTokens,
		Tools:                sharedconfig.DefaultToolResultHookTools(),
		WorkspaceRoot:        strings.TrimSpace(workspaceRoot),
	}
}

// Normalized 合并零值与默认值。
func (c Config) Normalized() Config {
	out := c
	if out.SpillThresholdTokens <= 0 {
		out.SpillThresholdTokens = DefaultSpillThresholdTokens
	}
	out.WorkspaceRoot = strings.TrimSpace(out.WorkspaceRoot)
	return out
}

func (c Config) appliesTo(toolName string) bool {
	name := strings.TrimSpace(toolName)
	if name == "" {
		return false
	}
	for _, t := range c.Tools {
		if strings.TrimSpace(t) == name {
			return true
		}
	}
	return false
}
