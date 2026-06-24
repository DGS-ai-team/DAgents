package compression

import (
	"log/slog"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

// LastCompressionSnapshot 为 session 最近一次成功写回压缩的侧车 usage 快照。
// 字段对齐 DeepSeek Chat Completions usage（见 API 文档）。
type LastCompressionSnapshot struct {
	TriggerLevel           string    `json:"trigger_level,omitempty"`
	Status                 string    `json:"status,omitempty"`
	AppliedAt              time.Time `json:"applied_at,omitempty"`
	CompressedMessageCount int       `json:"compressed_message_count,omitempty"`
	CompressionStart       int       `json:"compression_start,omitempty"`
	CompressionEnd         int       `json:"compression_end,omitempty"`
	PromptTokens           int       `json:"prompt_tokens,omitempty"`
	CompletionTokens       int       `json:"completion_tokens,omitempty"`
	TotalTokens            int       `json:"total_tokens,omitempty"`
	TokenReductionRate     float64   `json:"token_reduction_rate,omitempty"`
	PromptCacheHitTokens   int       `json:"prompt_cache_hit_tokens,omitempty"`
	PromptCacheMissTokens  int       `json:"prompt_cache_miss_tokens,omitempty"`
	PromptCacheHitRate     float64   `json:"prompt_cache_hit_rate,omitempty"`
}

func buildLastCompressionSnapshot(ready readyCompression, usage llm.Usage) LastCompressionSnapshot {
	usage.Normalize()
	snap := LastCompressionSnapshot{
		TriggerLevel:           ready.TriggerLevel,
		Status:                 "applied",
		AppliedAt:              time.Now().UTC(),
		CompressedMessageCount: ready.CompressedMessageCount,
		CompressionStart:       0,
		CompressionEnd:         ready.End,
		PromptTokens:           max(0, usage.PromptTokens),
		CompletionTokens:       max(0, usage.CompletionTokens),
		TotalTokens:            max(0, usage.TotalTokens),
		TokenReductionRate:     tokenReductionRate(usage.PromptTokens, usage.CompletionTokens),
		PromptCacheHitTokens:   usage.PromptCachedTokens(),
		PromptCacheMissTokens:  usage.PromptCacheMissTokensEffective(),
	}
	if rate := usage.PromptCacheHitRate(); rate >= 0 {
		snap.PromptCacheHitRate = rate
	}
	return snap
}

func (c *Coordinator) recordLastCompression(sessionID string, snap LastCompressionSnapshot) {
	if c == nil || sessionID == "" {
		return
	}
	c.mu.Lock()
	if c.lastCompressions == nil {
		c.lastCompressions = make(map[string]LastCompressionSnapshot)
	}
	c.lastCompressions[sessionID] = snap
	logger := c.logger
	c.mu.Unlock()

	if logger == nil {
		return
	}
	attrs := []any{
		"session_id", sessionID,
		"trigger_level", snap.TriggerLevel,
		"compressed_message_count", snap.CompressedMessageCount,
		"compression_start", snap.CompressionStart,
		"compression_end", snap.CompressionEnd,
		"prompt_tokens", snap.PromptTokens,
		"completion_tokens", snap.CompletionTokens,
		"token_reduction_rate", snap.TokenReductionRate,
		"prompt_cache_hit_tokens", snap.PromptCacheHitTokens,
		"prompt_cache_miss_tokens", snap.PromptCacheMissTokens,
	}
	if snap.PromptCacheHitRate >= 0 {
		attrs = append(attrs, "prompt_cache_hit_rate", snap.PromptCacheHitRate)
	}
	logger.Info("context compression applied", attrs...)
}

// LastCompression 返回 session 最近一次成功写回压缩的快照；无记录时 ok=false。
func (c *Coordinator) LastCompression(sessionID string) (LastCompressionSnapshot, bool) {
	if c == nil || sessionID == "" {
		return LastCompressionSnapshot{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	snap, ok := c.lastCompressions[sessionID]
	return snap, ok
}

// SetLogger 注入结构化日志；成功写回压缩时记录 cache usage。
func (c *Coordinator) SetLogger(logger *slog.Logger) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.logger = logger
	c.mu.Unlock()
}

// SetRawMessageHistoryEnabled 控制压缩摘要末尾是否追加 JSONL 审计文件路径指引。
func (c *Coordinator) SetRawMessageHistoryEnabled(enabled bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.rawMessageHistoryEnabled = enabled
	c.mu.Unlock()
}
