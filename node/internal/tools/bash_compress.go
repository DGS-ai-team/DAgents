package tools

import (
	"context"
	"regexp"
	"strings"
	"unicode/utf8"
)

// BashCompressConfig 控制 bash_run 输出压缩（P0：L1 清洗 + rune 安全截断）。
type BashCompressConfig struct {
	Enabled               bool
	MaxOutputChars        int // stdout 最大 rune 数；0 表示默认 maxBashOutputRunes
	MaxOutputCharsStderr  int // stderr 最大 rune 数；0 表示与 stdout 相同
	OutputMode            string
	TailOutputChars       int // head_tail 模式下 stdout 尾部最大 rune 数
	TailOutputCharsStderr int // head_tail 模式下 stderr 尾部最大 rune 数；0 表示与 stdout 相同
}

// DefaultBashCompressConfig 为 P0 默认：开启清洗，stdout 12000 / stderr 16000 runes。
func DefaultBashCompressConfig() BashCompressConfig {
	return BashCompressConfig{
		Enabled:              true,
		MaxOutputChars:       maxBashOutputRunes,
		MaxOutputCharsStderr: maxBashOutputStderrRunes,
		OutputMode:           "head",
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
	out.OutputMode = strings.ToLower(strings.TrimSpace(out.OutputMode))
	if out.OutputMode == "" {
		out.OutputMode = "head"
	}
	if out.OutputMode != "head" && out.OutputMode != "head_tail" {
		out.OutputMode = "head"
	}
	if out.TailOutputCharsStderr <= 0 {
		out.TailOutputCharsStderr = out.TailOutputChars
	}
	if out.TailOutputChars < 0 {
		out.TailOutputChars = 0
	}
	if out.TailOutputCharsStderr < 0 {
		out.TailOutputCharsStderr = 0
	}
	return out
}

// newBashOutputBuffer bounds bytes at the collection boundary. The later
// rune-level compression is still needed for readable tool results, but it
// must not be the first place where an unbounded command output is handled.
func newBashOutputBuffer(cfg BashCompressConfig, stderr bool) *OutputBudget {
	cfg = cfg.normalized()
	maxRunes := cfg.MaxOutputChars
	if stderr {
		maxRunes = cfg.MaxOutputCharsStderr
	}
	maxInt := int(^uint(0) >> 1)
	limit := maxInt
	if maxRunes > 0 && maxRunes <= maxInt/4 {
		limit = maxRunes * 4
	}
	if cfg.OutputMode == "head_tail" {
		tailRunes := cfg.TailOutputChars
		if stderr {
			tailRunes = cfg.TailOutputCharsStderr
		}
		if tailRunes > 0 && tailRunes <= maxInt/4 {
			return NewHeadTailOutputBudget(limit, tailRunes*4)
		}
		if tailRunes <= 0 {
			return NewOutputBudget(limit)
		}
		return NewHeadTailOutputBudget(limit, maxInt)
	}
	return NewOutputBudget(limit)
}

// SetBashCompress 注入 bash 输出压缩配置（由 Node 启动时从 config.yaml 映射）。
func (r *Registry) SetBashCompress(cfg BashCompressConfig) {
	r.bashCompress = cfg.normalized()
}

type bashStreamCompressMeta struct {
	enabled       bool
	inRunes       int
	outRunes      int
	sanitized     bool
	runeTruncated bool
}

func sanitizeBashStream(cfg BashCompressConfig, text string) (string, bashStreamCompressMeta) {
	inRunes := utf8.RuneCountInString(text)
	meta := bashStreamCompressMeta{
		enabled: cfg.Enabled,
		inRunes: inRunes,
	}
	if text == "" {
		return text, meta
	}

	out := strings.TrimSpace(text)
	if cfg.Enabled {
		before := out
		out = sanitizeCLIOutput(out)
		meta.sanitized = out != before
	}
	meta.outRunes = utf8.RuneCountInString(out)
	return out, meta
}

func compressBashStream(cfg BashCompressConfig, text string, maxRunes int) (string, bashStreamCompressMeta) {
	out, meta := sanitizeBashStream(cfg, text)
	if maxRunes > 0 {
		out, meta.runeTruncated = clipTextRunes(out, maxRunes)
		meta.outRunes = utf8.RuneCountInString(out)
	}
	return out, meta
}

var ansiEscapeRE = regexp.MustCompile(`\x1b\[[0-9:;?]*[ -/]*[@-~]|\x1b\][^\x07]*(?:\x07|\x1b\\)`)

func sanitizeCLIOutput(s string) string {
	if s == "" {
		return s
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = ansiEscapeRE.ReplaceAllString(s, "")

	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	var prev string
	prevSet := false
	repeatCount := 0

	flushRepeat := func() {
		if repeatCount <= 0 {
			return
		}
		if repeatCount == 1 {
			out = append(out, prev)
		} else {
			out = append(out, prev, repeatLineNote(repeatCount))
		}
		repeatCount = 0
		prevSet = false
	}

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			flushRepeat()
			if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
				out = append(out, "")
			}
			continue
		}
		if prevSet && line == prev {
			repeatCount++
			continue
		}
		flushRepeat()
		prev = line
		prevSet = true
		repeatCount = 1
	}
	flushRepeat()

	for len(out) > 0 && strings.TrimSpace(out[0]) == "" {
		out = out[1:]
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n")
}

func repeatLineNote(count int) string {
	return "[... repeated " + itoa(count) + " identical lines omitted ...]"
}

func itoa(n int) string {
	if n <= 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func clipTextRunes(s string, maxRunes int) (string, bool) {
	if maxRunes <= 0 || s == "" {
		return s, false
	}
	if utf8.RuneCountInString(s) <= maxRunes {
		return s, false
	}
	var b strings.Builder
	b.Grow(len(s))
	n := 0
	for _, r := range s {
		if n >= maxRunes {
			break
		}
		b.WriteRune(r)
		n++
	}
	return b.String(), true
}

// OutputCompressStats 为 bash_run 输出压缩统计（仅在有节省时写入 SSE）。
type OutputCompressStats struct {
	RawRunes  int
	OutRunes  int
	SavedPct  int
	Truncated bool
}

func (s *OutputCompressStats) SSEFields() map[string]any {
	if s == nil || (s.SavedPct <= 0 && !s.Truncated) {
		return nil
	}
	fields := map[string]any{
		"output_compress_raw_runes": s.RawRunes,
		"output_compress_out_runes": s.OutRunes,
		"output_compress_saved_pct": s.SavedPct,
	}
	if s.Truncated {
		fields["output_compress_truncated"] = true
	}
	return fields
}

func aggregateBashCompressStats(outMeta, errMeta bashStreamCompressMeta) *OutputCompressStats {
	raw := outMeta.inRunes + errMeta.inRunes
	out := outMeta.outRunes + errMeta.outRunes
	truncated := outMeta.runeTruncated || errMeta.runeTruncated
	sanitized := outMeta.sanitized || errMeta.sanitized
	if raw <= 0 {
		return nil
	}
	if out >= raw && !truncated && !sanitized {
		return nil
	}
	saved := raw - out
	if saved < 0 {
		saved = 0
	}
	pct := saved * 100 / raw
	if pct <= 0 && !truncated {
		return nil
	}
	if pct <= 0 && truncated {
		pct = 1
	}
	return &OutputCompressStats{
		RawRunes:  raw,
		OutRunes:  out,
		SavedPct:  pct,
		Truncated: truncated,
	}
}

type toolCallContextKey struct{}

func WithToolCallID(ctx context.Context, toolCallID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	id := strings.TrimSpace(toolCallID)
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, toolCallContextKey{}, id)
}

func toolCallIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(toolCallContextKey{}).(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func (r *Registry) stashBashCompressStats(toolCallID string, stats *OutputCompressStats) {
	if stats == nil {
		return
	}
	id := strings.TrimSpace(toolCallID)
	if id == "" {
		return
	}
	r.compressMu.Lock()
	if r.bashCompressStats == nil {
		r.bashCompressStats = make(map[string]*OutputCompressStats)
	}
	r.bashCompressStats[id] = stats
	r.compressMu.Unlock()
}

func (r *Registry) TakeBashCompressStatsForCall(toolCallID string) map[string]any {
	id := strings.TrimSpace(toolCallID)
	if id == "" {
		return nil
	}
	r.compressMu.Lock()
	stats := r.bashCompressStats[id]
	delete(r.bashCompressStats, id)
	r.compressMu.Unlock()
	if stats == nil {
		return nil
	}
	return stats.SSEFields()
}
