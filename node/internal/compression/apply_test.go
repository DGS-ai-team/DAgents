package compression

import (
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func TestApplyCompressionReplacementKeepAssistant(t *testing.T) {
	t.Parallel()

	msgs := []llm.Message{
		{Role: "user", Content: "a"},
		{Role: "assistant", Content: "b"},
		{Role: "user", Content: "c"},
		{Role: "assistant", Content: "tail"},
	}
	plan, ok := buildCompressionPlan(msgs)
	if !ok {
		t.Fatal("expected plan")
	}
	fp := compressionSourceFingerprint(msgs, plan)
	merged, status := applyCompressionReplacement(msgs, plan, "summary", fp)
	if status != "applied" {
		t.Fatalf("status = %q", status)
	}
	if len(merged) != 2 {
		t.Fatalf("len = %d, msgs = %+v", len(merged), merged)
	}
	if merged[0].Role != "user" || merged[0].Content != "summary" || merged[0].Name != llm.UserNameCompression {
		t.Fatalf("replacement = %+v", merged[0])
	}
	if merged[1].Role != "assistant" || merged[1].Content != "tail" {
		t.Fatalf("tail assistant = %+v", merged[1])
	}
}

func TestApplyCompressionReplacementMergeNextUser(t *testing.T) {
	t.Parallel()

	msgs := []llm.Message{
		{Role: "user", Content: "task"},
		{Role: "assistant", Content: "done"},
		{Role: "user", Content: "next"},
	}
	plan, ok := buildCompressionPlan(msgs)
	if !ok {
		t.Fatal("expected plan")
	}
	fp := compressionSourceFingerprint(msgs, plan)
	merged, status := applyCompressionReplacement(msgs, plan, "summary", fp)
	if status != "applied" {
		t.Fatalf("status = %q", status)
	}
	if len(merged) != 1 {
		t.Fatalf("len = %d, msgs = %+v", len(merged), merged)
	}
	want := "summary\n\nnext"
	if merged[0].Role != "user" || merged[0].Content != want || merged[0].Name != llm.UserNameCompression {
		t.Fatalf("merged user = %+v", merged[0])
	}
}

func TestApplyCompressionReplacementMergeNextUserStale(t *testing.T) {
	t.Parallel()

	msgs := []llm.Message{
		{Role: "user", Content: "task"},
		{Role: "assistant", Content: "done"},
		{Role: "user", Content: "next"},
	}
	plan, ok := buildCompressionPlan(msgs)
	if !ok {
		t.Fatal("expected plan")
	}
	fp := compressionSourceFingerprint(msgs, plan)
	msgs[2].Content = "changed"
	_, status := applyCompressionReplacement(msgs, plan, "summary", fp)
	if status != "stale" {
		t.Fatalf("status = %q, want stale", status)
	}
}
