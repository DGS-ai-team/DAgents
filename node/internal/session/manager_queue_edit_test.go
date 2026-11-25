package session

import (
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func TestQueuedTextEditReplacesOldTextWhileRetainingAttachments(t *testing.T) {
	previous := []llm.ContentPart{
		{Type: "text", Text: "original instruction"},
		{Type: "image_url", ImageURL: &llm.ImageURLPart{URL: "https://example.test/photo.png"}},
	}
	parts := retainedQueuedAttachments(previous)
	summary, normalized, err := llm.NormalizeUserInput("edited instruction", parts)
	if err != nil {
		t.Fatal(err)
	}
	if summary != "edited instruction" {
		t.Fatalf("normalized text = %q, want edited instruction", summary)
	}
	if len(normalized) != 2 || normalized[0].Type != "text" || normalized[0].Text != "edited instruction" || normalized[1].Type != "image_url" {
		t.Fatalf("normalized edit parts = %#v, want edited text followed by retained image", normalized)
	}
}
