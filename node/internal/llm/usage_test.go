package llm

import (
	"encoding/json"
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
}
