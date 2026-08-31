package turn

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

func TestExecuteTool_readImageAppendsVisionUserMessage(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "chart.png")
	if err := os.WriteFile(imgPath, []byte("png-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := tools.NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetMultimodalEnabled(true)
	hub := stream.NewHub(8, logx.Discard())
	pol, _ := policy.LoadFile("")
	orch := NewOrchestrator("agent-1", dir, hub, &llm.MockClient{}, reg, pol, SkillAccess{}, DefaultMaxToolLoops(), nil, nil, hooksRuntimeConfig(t), logx.Discard())
	orch.SetMultimodalEnabled(true)

	history := []llm.Message{
		{Role: "user", Content: "describe chart"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{
			ID: "call-img-1", Type: "function",
			Function: llm.ToolCallFunction{
				Name:      "read_image",
				Arguments: `{"path":"chart.png","call_purpose":"inspect chart"}`,
			},
		}}},
	}
	tc := history[1].ToolCalls[0]
	if err := orch.executeTool(context.Background(), "sess-vision", &history, tc, nil); err != nil {
		t.Fatal(err)
	}
	if len(history) != 4 {
		t.Fatalf("history len = %d, want user+assistant+tool+vision user", len(history))
	}
	if history[2].Role != "tool" || !strings.Contains(history[2].Content, "[READ_IMAGE]") {
		t.Fatalf("tool msg = %+v", history[2])
	}
	if history[3].Role != "user" || history[3].Name != llm.UserNameToolVision {
		t.Fatalf("vision user = %+v", history[3])
	}
	if len(history[3].ContentParts) != 2 || history[3].ContentParts[1].Type != "image_url" {
		t.Fatalf("content parts = %+v", history[3].ContentParts)
	}
}

func TestExecuteTool_readImageSkipsVisionWhenMultimodalDisabled(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "chart.png")
	if err := os.WriteFile(imgPath, []byte("png-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := tools.NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetMultimodalEnabled(true)
	hub := stream.NewHub(8, logx.Discard())
	pol, _ := policy.LoadFile("")
	orch := NewOrchestrator("agent-1", dir, hub, &llm.MockClient{}, reg, pol, SkillAccess{}, DefaultMaxToolLoops(), nil, nil, hooksRuntimeConfig(t), logx.Discard())

	history := []llm.Message{
		{Role: "user", Content: "describe chart"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{
			ID: "call-img-2", Type: "function",
			Function: llm.ToolCallFunction{
				Name:      "read_image",
				Arguments: `{"path":"chart.png","call_purpose":"inspect chart"}`,
			},
		}}},
	}
	tc := history[1].ToolCalls[0]
	if err := orch.executeTool(context.Background(), "sess-vision-off", &history, tc, nil); err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 {
		t.Fatalf("history len = %d, want user+assistant+tool without vision user", len(history))
	}
	if history[2].Role != "tool" || !strings.Contains(history[2].Content, "[READ_IMAGE]") {
		t.Fatalf("tool msg = %+v", history[2])
	}
}

func TestBuildToolVisionUserMessageGroupsImages(t *testing.T) {
	msg, err := buildToolVisionUserMessage([]*tools.ReadImageVisionPayload{
		{
			RelPath: "one.png",
			Detail:  "low",
			DataURL: "data:image/png;base64,b25l",
			Prompt:  "inspect first",
			FrameID: "frame-one",
		},
		{
			RelPath: "two.png",
			Detail:  "high",
			DataURL: "data:image/png;base64,dHdv",
			Prompt:  "inspect second",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Role != "user" || msg.Name != llm.UserNameToolVision {
		t.Fatalf("message = %+v", msg)
	}
	if len(msg.ContentParts) != 4 {
		t.Fatalf("content parts = %d, want 4", len(msg.ContentParts))
	}
	if msg.ContentParts[1].Type != "image_url" || msg.ContentParts[3].Type != "image_url" {
		t.Fatalf("content parts = %+v", msg.ContentParts)
	}
	if !strings.Contains(msg.ContentParts[0].Text, "frame-one") {
		t.Fatalf("first prompt = %q, want frame id", msg.ContentParts[0].Text)
	}
}

func hooksRuntimeConfig(t *testing.T) hooks.RuntimeConfig {
	t.Helper()
	return hooks.RuntimeConfig{
		Duplicate:  hooks.DefaultDuplicateConfig(),
		ToolResult: hooks.DefaultToolResultConfig(t.TempDir()),
	}
}
