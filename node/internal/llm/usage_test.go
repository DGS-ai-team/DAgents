package llm

import (
	"encoding/json"
	"math"
	"testing"
)

func TestUsageUnmarshalJSONWithCacheDetails(t *testing.T) {
	raw := `{
		"prompt_tokens": 100,
		"completion_tokens": 20,
		"total_tokens": 120,
		"prompt_cache_hit_tokens": 80,
		"prompt_cache_miss_tokens": 20,
		"prompt_tokens_details": {
			"cached_tokens": 80,
			"audio_tokens": 3
		}
	}`
	var u Usage
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatal(err)
	}
	if u.PromptTokens != 100 || u.CompletionTokens != 20 || u.TotalTokens != 120 {
		t.Fatalf("base tokens = %+v", u)
	}
	if u.PromptCacheHitTokens != 80 || u.PromptCacheMissTokens != 20 {
		t.Fatalf("cache hit/miss = %+v", u)
	}
	if u.PromptCachedTokens() != 80 || u.PromptAudioTokens() != 3 {
		t.Fatalf("details = cached %d audio %d", u.PromptCachedTokens(), u.PromptAudioTokens())
	}
	payload := u.SSEPayload()
	if payload["prompt_cache_hit_tokens"] != 80 {
		t.Fatalf("payload = %#v", payload)
	}
	if payload["prompt_cached_tokens"] != 80 {
		t.Fatalf("payload cached = %#v", payload)
	}
	if payload["prompt_audio_tokens"] != 3 {
		t.Fatalf("payload audio = %#v", payload)
	}
	rate, ok := payload["prompt_cache_hit_rate"].(float64)
	if !ok || math.Abs(rate-0.8) > 1e-9 {
		t.Fatalf("hit rate = %#v", payload["prompt_cache_hit_rate"])
	}
}

func TestUsageUnmarshalJSONPromptTokenDetailsAlias(t *testing.T) {
	raw := `{
		"prompt_tokens": 10,
		"completion_tokens": 2,
		"total_tokens": 12,
		"prompt_token_details": {
			"cached_tokens": 6
		}
	}`
	var u Usage
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatal(err)
	}
	if u.PromptCachedTokens() != 6 {
		t.Fatalf("cached = %d", u.PromptCachedTokens())
	}
	if u.PromptCacheHitTokens != 6 {
		t.Fatalf("hit = %d", u.PromptCacheHitTokens)
	}
	if u.PromptCacheMissTokensEffective() != 4 {
		t.Fatalf("miss = %d", u.PromptCacheMissTokensEffective())
	}
}

func TestUsageNormalizeDeepSeekOnlyFields(t *testing.T) {
	raw := `{
		"prompt_tokens": 105,
		"completion_tokens": 50,
		"total_tokens": 155,
		"prompt_cache_hit_tokens": 64,
		"prompt_cache_miss_tokens": 41
	}`
	var u Usage
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatal(err)
	}
	if u.PromptCachedTokens() != 64 {
		t.Fatalf("cached = %d", u.PromptCachedTokens())
	}
	if u.PromptTokensDetails == nil || u.PromptTokensDetails.CachedTokens != 64 {
		t.Fatalf("details = %+v", u.PromptTokensDetails)
	}
}

func TestUsageNormalizeOpenAIReasoningTokens(t *testing.T) {
	raw := `{
		"prompt_tokens": 2006,
		"completion_tokens": 300,
		"total_tokens": 2306,
		"prompt_tokens_details": {"cached_tokens": 1920},
		"completion_tokens_details": {"reasoning_tokens": 128}
	}`
	var u Usage
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatal(err)
	}
	payload := u.SSEPayload()
	if payload["reasoning_tokens"] != 128 {
		t.Fatalf("reasoning = %#v", payload["reasoning_tokens"])
	}
	if payload["prompt_cache_hit_tokens"] != 1920 {
		t.Fatalf("hit = %#v", payload["prompt_cache_hit_tokens"])
	}
}

func TestUsageUnmarshalJSONTopLevelReasoningTokens(t *testing.T) {
	raw := `{
		"prompt_tokens": 5,
		"completion_tokens": 238,
		"total_tokens": 243,
		"reasoning_tokens": 93
	}`
	var u Usage
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatal(err)
	}
	if got := u.CompletionReasoningTokens(); got != 93 {
		t.Fatalf("reasoning = %d", got)
	}
}

func TestUsageUnmarshalJSONCompletionTokenDetailsAlias(t *testing.T) {
	raw := `{
		"prompt_tokens": 5,
		"completion_tokens": 238,
		"completion_token_details": {"reasoning_tokens": 77}
	}`
	var u Usage
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatal(err)
	}
	if got := u.CompletionReasoningTokens(); got != 77 {
		t.Fatalf("reasoning = %d", got)
	}
}

func TestUsageSSEEventRoundAndTurnFields(t *testing.T) {
	var turn Usage
	turn.AccumulateFrom(Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120})
	round := Usage{PromptTokens: 40, CompletionTokens: 8, TotalTokens: 48}
	payload := UsageSSEEvent(2, round, turn)
	if payload["llm_step"] != 2 {
		t.Fatalf("llm_step = %v", payload["llm_step"])
	}
	if payload["prompt_tokens"] != 100 || payload["completion_tokens"] != 20 {
		t.Fatalf("turn fields = %v", payload)
	}
	if payload["round_prompt_tokens"] != 40 || payload["round_completion_tokens"] != 8 {
		t.Fatalf("round fields = %v", payload)
	}
}

func TestUsageAccumulateFrom(t *testing.T) {
	var acc Usage
	acc.AccumulateFrom(Usage{
		PromptTokens:     100,
		CompletionTokens: 20,
		TotalTokens:      120,
		CompletionTokensDetails: &CompletionTokensDetails{ReasoningTokens: 15},
	})
	acc.AccumulateFrom(Usage{
		PromptTokens:     50,
		CompletionTokens: 10,
		TotalTokens:      60,
		CompletionTokensDetails: &CompletionTokensDetails{ReasoningTokens: 8},
	})
	payload := acc.SSEPayload()
	if payload["prompt_tokens"] != 150 || payload["completion_tokens"] != 30 {
		t.Fatalf("tokens = %#v", payload)
	}
	if payload["reasoning_tokens"] != 23 {
		t.Fatalf("reasoning = %#v", payload["reasoning_tokens"])
	}
}

func TestUsageNormalizeComputesMissFromPromptMinusHit(t *testing.T) {
	u := Usage{PromptTokens: 100, PromptCacheHitTokens: 80}
	u.Normalize()
	if u.PromptCacheMissTokens != 20 {
		t.Fatalf("miss = %d", u.PromptCacheMissTokens)
	}
}
