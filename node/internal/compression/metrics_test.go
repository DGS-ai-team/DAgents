package compression

import (
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func TestTokenReductionRate(t *testing.T) {
	t.Parallel()

	if got := tokenReductionRate(1000, 400); got != 0.6 {
		t.Fatalf("got %v", got)
	}
	if tokenReductionRate(0, 10) != 0 {
		t.Fatal("zero prompt")
	}
	if tokenReductionRate(100, 100) != 0 {
		t.Fatal("no reduction")
	}
	if tokenReductionRate(100, 150) != 0 {
		t.Fatal("completion larger than prompt")
	}
}

func TestAttachCompressionUsageMetrics(t *testing.T) {
	t.Parallel()

	payload := map[string]any{"status": "applied"}
	attachCompressionUsageMetrics(payload, llm.Usage{
		PromptTokens:          1000,
		CompletionTokens:      400,
		TotalTokens:           1400,
		PromptCacheHitTokens:  800,
		PromptCacheMissTokens: 200,
	})
	if payload["prompt_tokens"] != 1000 || payload["completion_tokens"] != 400 {
		t.Fatalf("tokens = %+v", payload)
	}
	if payload["token_reduction_rate"] != 0.6 {
		t.Fatalf("rate = %v", payload["token_reduction_rate"])
	}
	if payload["prompt_cache_hit_tokens"] != 800 {
		t.Fatalf("hit = %v", payload["prompt_cache_hit_tokens"])
	}
	if payload["prompt_cache_miss_tokens"] != 200 {
		t.Fatalf("miss = %v", payload["prompt_cache_miss_tokens"])
	}
	if payload["prompt_cache_available"] != true {
		t.Fatalf("availability = %v", payload["prompt_cache_available"])
	}
}
