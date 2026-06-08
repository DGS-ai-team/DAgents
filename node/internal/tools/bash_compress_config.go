package tools

// BashCompressConfig 控制 bash_run 输出压缩（P0：L1 清洗 + rune 安全截断）。
type BashCompressConfig struct {
	Enabled              bool
	MaxOutputChars       int // stdout 最大 rune 数；0 表示默认 maxBashOutputRunes
	MaxOutputCharsStderr int // stderr 最大 rune 数；0 表示与 stdout 相同
}

// DefaultBashCompressConfig 为 P0 默认：开启清洗，stdout 12000 / stderr 16000 runes。
func DefaultBashCompressConfig() BashCompressConfig {
	return BashCompressConfig{
		Enabled:              true,
		MaxOutputChars:       maxBashOutputRunes,
		MaxOutputCharsStderr: maxBashOutputStderrRunes,
	}
}

func (c BashCompressConfig) normalized() BashCompressConfig {
	out := c
	if out.MaxOutputChars <= 0 {
		out.MaxOutputChars = maxBashOutputRunes
	}
	if out.MaxOutputCharsStderr <= 0 {
		out.MaxOutputCharsStderr = maxBashOutputStderrRunes
	}
	return out
}

// SetBashCompress 注入 bash 输出压缩配置（由 Node 启动时从 config.yaml 映射）。
func (r *Registry) SetBashCompress(cfg BashCompressConfig) {
	r.bashCompress = cfg.normalized()
}
