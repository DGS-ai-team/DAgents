package llm

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestNormalizeUserInput_textOnly(t *testing.T) {
	text, parts, err := NormalizeUserInput("hello", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "hello" || len(parts) != 1 || parts[0].Text != "hello" {
		t.Fatalf("got text=%q parts=%+v", text, parts)
	}
}

func TestNormalizeUserInput_imageOnly(t *testing.T) {
	url := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("png-bytes"))
	text, parts, err := NormalizeUserInput("", []ContentPart{{
		Type:     "image_url",
		ImageURL: &ImageURLPart{URL: url},
	}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "" || len(parts) != 1 || parts[0].Type != "image_url" {
		t.Fatalf("got text=%q parts=%+v", text, parts)
	}
}

func TestNormalizeUserInput_mergeTextAndParts(t *testing.T) {
	url := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("png-bytes"))
	text, parts, err := NormalizeUserInput("describe", []ContentPart{{
		Type:     "image_url",
		ImageURL: &ImageURLPart{URL: url},
	}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "describe" || len(parts) != 2 {
		t.Fatalf("got text=%q parts=%+v", text, parts)
	}
	if parts[0].Type != "text" || parts[1].Type != "image_url" {
		t.Fatalf("unexpected order: %+v", parts)
	}
}

func TestMessageToAPIPayload_multimodal(t *testing.T) {
	msg, err := BuildUserMessage("look", []ContentPart{{
		Type:     "image_url",
		ImageURL: &ImageURLPart{URL: "https://example.com/a.png"},
	}}, UserNameHuman)
	if err != nil {
		t.Fatalf("BuildUserMessage: %v", err)
	}
	payload, err := MessageToAPIPayload(msg)
	if err != nil {
		t.Fatalf("MessageToAPIPayload: %v", err)
	}
	content, ok := payload["content"].([]map[string]any)
	if !ok {
		t.Fatalf("content type = %T", payload["content"])
	}
	if len(content) != 2 {
		t.Fatalf("parts = %d", len(content))
	}
}

func TestMessageToDeepSeekAPIPayload_multimodal(t *testing.T) {
	msg := Message{
		Role: "user",
		ContentParts: []ContentPart{
			{Type: "text", Text: "hi"},
			{Type: "image_url", ImageURL: &ImageURLPart{URL: "https://example.com/a.png"}},
		},
	}
	payload, err := MessageToDeepSeekAPIPayload(msg)
	if err != nil {
		t.Fatalf("MessageToDeepSeekAPIPayload: %v", err)
	}
	parts, ok := payload["content"].([]map[string]any)
	if !ok || len(parts) != 2 {
		t.Fatalf("content = %#v", payload["content"])
	}
}

func TestPrepareMessagesForTextOnly_stripsImagesFromOutboundCopy(t *testing.T) {
	withText := Message{
		Role:    "user",
		Content: "describe",
		ContentParts: []ContentPart{
			{Type: "text", Text: "describe"},
			{Type: "image_url", ImageURL: &ImageURLPart{URL: "https://example.com/a.png"}},
		},
	}
	imageOnly := Message{
		Role: "user",
		ContentParts: []ContentPart{
			{Type: "image_url", ImageURL: &ImageURLPart{URL: "https://example.com/b.png"}},
		},
	}
	history := []Message{withText, imageOnly}
	out := PrepareMessagesForTextOnly(history)
	if MessageHasImages(out[0]) || MessageHasImages(out[1]) {
		t.Fatalf("outbound messages still contain images: %+v", out)
	}
	if out[0].Content != "describe" || out[1].Content != textOnlyImageOmissionNotice {
		t.Fatalf("outbound text = %#v", []string{out[0].Content, out[1].Content})
	}
	if !MessageHasImages(history[0]) || !MessageHasImages(history[1]) {
		t.Fatal("text-only preparation mutated persisted history")
	}
}

func TestValidateImage_rejectsLargeDataURL(t *testing.T) {
	large := make([]byte, MaxImageBytes+1)
	url := "data:image/png;base64," + base64.StdEncoding.EncodeToString(large)
	_, _, err := NormalizeUserInput("", []ContentPart{{
		Type:     "image_url",
		ImageURL: &ImageURLPart{URL: url},
	}})
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected size error, got %v", err)
	}
}
