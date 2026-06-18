package compression

import (
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func TestBuildLastCompressionSnapshot(t *testing.T) {
	t.Parallel()

	snap := buildLastCompressionSnapshot(readyCompression{
		End:                    3,
		TriggerLevel:           "blocking",
		CompressedMessageCount: 4,
	}, llm.Usage{
		PromptTokens:          1000,
		CompletionTokens:      400,
		TotalTokens:           1400,
		PromptCacheHitTokens:  800,
		PromptCacheMissTokens: 200,
	})
	if snap.Status != "applied" || snap.TriggerLevel != "blocking" {
		t.Fatalf("meta = %+v", snap)
	}
	if snap.PromptTokens != 1000 || snap.CompletionTokens != 400 {
		t.Fatalf("tokens = %+v", snap)
	}
	if snap.TokenReductionRate != 0.6 {
		t.Fatalf("rate = %v", snap.TokenReductionRate)
	}
	if snap.PromptCacheHitTokens != 800 || snap.PromptCacheMissTokens != 200 {
		t.Fatalf("cache = %+v", snap)
	}
	if snap.PromptCacheHitRate != 0.8 {
		t.Fatalf("hit rate = %v", snap.PromptCacheHitRate)
	}
	if snap.AppliedAt.IsZero() {
		t.Fatal("expected applied_at")
	}
}

func TestLastCompressionRoundTrip(t *testing.T) {
	t.Parallel()

	c := NewCoordinator(&countingLLM{}, 10, 20)
	c.mu.Lock()
	if c.lastCompressions == nil {
		c.lastCompressions = make(map[string]LastCompressionSnapshot)
	}
	c.lastCompressions["sess-1"] = LastCompressionSnapshot{
		Status:           "applied",
		PromptTokens:     500,
		CompletionTokens: 100,
	}
	c.mu.Unlock()

	snap, ok := c.LastCompression("sess-1")
	if !ok || snap.PromptTokens != 500 {
		t.Fatalf("got %+v ok=%v", snap, ok)
	}
	if _, ok := c.LastCompression("missing"); ok {
		t.Fatal("expected missing session")
	}
}
