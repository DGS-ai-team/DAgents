package media

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func TestRehydrateFromMessages_showImage(t *testing.T) {
	dir := t.TempDir()
	pngPath := filepath.Join(dir, "chart.png")
	if err := os.WriteFile(pngPath, []byte{0x89, 0x50, 0x4e, 0x47}, 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := NewRegistry("sess-1", dir)
	if err != nil {
		t.Fatal(err)
	}
	messages := []llm.Message{
		{
			Role: "assistant",
			ToolCalls: []llm.ToolCall{{
				ID:   "call-1",
				Type: "function",
				Function: llm.ToolCallFunction{
					Name:      "show_image",
					Arguments: `{"path":"chart.png","call_purpose":"展示"}`,
				},
			}},
		},
		{
			Role:       "tool",
			ToolCallID: "call-1",
			Name:       "show_image",
			Content:    "[SHOW_IMAGE]\npath=chart.png\nstatus=ok",
		},
	}
	callIndex := map[string]llm.ToolCall{"call-1": messages[0].ToolCalls[0]}
	out := RehydrateFromMessages(reg, messages, callIndex)
	items := out["call-1"]
	if len(items) != 1 || items[0]["url"] == "" {
		t.Fatalf("media=%v", out)
	}
}
