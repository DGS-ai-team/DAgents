package toolresult

import "strings"

const (
	// DefaultMaxHistoryTokens 写入 history 的 token 预算（DeepSeek 粗算：中文×0.6、其它×0.3）。
	DefaultMaxHistoryTokens = 12000
	DefaultSpillSubdir      = ".runtime/tool_outputs"
)

// Config 控制 tool 结果写入 history 的摘要与落盘（WS3）。
type Config struct {
	Enabled         bool
	MaxHistoryTokens int
	SpillSubdir     string
	// Tools 为启用 spill/摘要 的工具名列表；空表示不处理任何工具。
	Tools  []string
	FSRoot string // 绝对路径，用于落盘
}

// DefaultConfig 返回 WS3 bash 组默认（仅 bash_run）。
func DefaultConfig(fsRoot string) Config {
	return Config{
		Enabled:         true,
		MaxHistoryTokens: DefaultMaxHistoryTokens,
		SpillSubdir:     DefaultSpillSubdir,
		Tools:           []string{"bash_run"},
		FSRoot:          strings.TrimSpace(fsRoot),
	}
}

// Normalized 合并零值与默认值。
func (c Config) Normalized() Config {
	out := c
	if out.MaxHistoryTokens <= 0 {
		out.MaxHistoryTokens = DefaultMaxHistoryTokens
	}
	if strings.TrimSpace(out.SpillSubdir) == "" {
		out.SpillSubdir = DefaultSpillSubdir
	}
	out.FSRoot = strings.TrimSpace(out.FSRoot)
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
