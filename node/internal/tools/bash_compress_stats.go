package tools

import (
	"context"
	"strings"
)

// OutputCompressStats 为 bash_run 输出压缩统计（仅在有节省时写入 SSE）。
type OutputCompressStats struct {
	RawRunes  int
	OutRunes  int
	SavedPct  int
	Truncated bool
}

// SSEFields 转为 tool_result SSE 附加字段。
func (s *OutputCompressStats) SSEFields() map[string]any {
	if s == nil || s.SavedPct <= 0 {
		return nil
	}
	return map[string]any{
		"output_compress_raw_runes":   s.RawRunes,
		"output_compress_out_runes":   s.OutRunes,
		"output_compress_saved_pct":   s.SavedPct,
	}
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

// WithToolCallID 将 tool_call_id 写入 context，供 bash_run 压缩统计关联。
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

// TakeBashCompressStatsForCall 取出并清除一次 bash 压缩 SSE 字段。
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

func outputCompressStatsFromSSEFields(fields map[string]any) *OutputCompressStats {
	if fields == nil {
		return nil
	}
	pct := intFromAny(fields["output_compress_saved_pct"])
	if pct <= 0 {
		return nil
	}
	return &OutputCompressStats{
		RawRunes: intFromAny(fields["output_compress_raw_runes"]),
		OutRunes: intFromAny(fields["output_compress_out_runes"]),
		SavedPct: pct,
	}
}

func intFromAny(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}
