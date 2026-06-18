package compression

import (
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

// tokenReductionRate 按 DeepSeek usage：prompt_tokens 为输入、completion_tokens 为摘要输出，
// 返回 [0,1] 的减少比例；(prompt-completion)/prompt。
func tokenReductionRate(promptTokens, completionTokens int) float64 {
	if promptTokens <= 0 || completionTokens >= promptTokens {
		return 0
	}
	return float64(promptTokens-completionTokens) / float64(promptTokens)
}

// attachCompressionUsageMetrics 将侧车 StreamChat 返回的 usage 写入压缩 SSE/API 载荷。
// 字段对齐 DeepSeek Chat Completions usage：prompt_tokens、completion_tokens、
// prompt_cache_hit_tokens、prompt_cache_miss_tokens（见 API 文档）。
func attachCompressionUsageMetrics(payload map[string]any, usage llm.Usage) {
	usage.Normalize()
	payload["prompt_tokens"] = max(0, usage.PromptTokens)
	payload["completion_tokens"] = max(0, usage.CompletionTokens)
	payload["total_tokens"] = max(0, usage.TotalTokens)
	payload["token_reduction_rate"] = tokenReductionRate(usage.PromptTokens, usage.CompletionTokens)
	payload["prompt_cache_hit_tokens"] = usage.PromptCachedTokens()
	payload["prompt_cache_miss_tokens"] = usage.PromptCacheMissTokensEffective()
	if rate := usage.PromptCacheHitRate(); rate >= 0 {
		payload["prompt_cache_hit_rate"] = rate
	}
}
