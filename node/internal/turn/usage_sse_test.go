package turn

import (
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func TestUsageSnapshotDeltaDoesNotRepeatCumulativeProviderSnapshot(t *testing.T) {
	first := llm.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120}
	second := llm.Usage{PromptTokens: 150, CompletionTokens: 30, TotalTokens: 180}

	delta1 := usageSnapshotDelta(first, llm.Usage{})
	delta2 := usageSnapshotDelta(second, first)
	repeat := usageSnapshotDelta(second, second)

	if delta1.PromptTokens != 100 || delta1.CompletionTokens != 20 || delta1.TotalTokens != 120 {
		t.Fatalf("first delta = %+v", delta1)
	}
	if delta2.PromptTokens != 50 || delta2.CompletionTokens != 10 || delta2.TotalTokens != 60 {
		t.Fatalf("second delta = %+v", delta2)
	}
	if repeat.PromptTokens != 0 || repeat.CompletionTokens != 0 || repeat.TotalTokens != 0 {
		t.Fatalf("repeated snapshot delta = %+v", repeat)
	}
}

func TestUsageSnapshotDeltaTreatsResetAsNewAttempt(t *testing.T) {
	previous := llm.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120}
	current := llm.Usage{PromptTokens: 40, CompletionTokens: 8, TotalTokens: 48}

	delta := usageSnapshotDelta(current, previous)
	if delta.PromptTokens != 40 || delta.CompletionTokens != 8 || delta.TotalTokens != 48 {
		t.Fatalf("reset delta = %+v", delta)
	}
}
