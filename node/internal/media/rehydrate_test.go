package media

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func TestRehydrateFromMessages_showImageAbsolutePath(t *testing.T) {
	fsRoot := t.TempDir()
	externalDir := t.TempDir()
	imgPath := filepath.Join(externalDir, "chart.png")
	if err := os.WriteFile(imgPath, []byte{0x89, 0x50, 0x4e, 0x47}, 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := NewRegistry("sess-1", fsRoot)
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
					Arguments: fmt.Sprintf(`{"path":%q,"call_purpose":"展示"}`, imgPath),
				},
			}},
		},
		{
			Role:       "tool",
			ToolCallID: "call-1",
			Name:       "show_image",
			Content:    fmt.Sprintf("[SHOW_IMAGE]\npath=%s\nstatus=ok", imgPath),
		},
	}
	callIndex := map[string]llm.ToolCall{"call-1": messages[0].ToolCalls[0]}
	out := RehydrateFromMessages(reg, messages, callIndex)
	items := out["call-1"]
	if len(items) != 1 || items[0]["url"] == "" {
		t.Fatalf("media=%v", out)
	}
	artID, _ := items[0]["id"].(string)
	if _, abs, err := reg.OpenFile(artID); err != nil || abs != imgPath {
		t.Fatalf("open external media: id=%q abs=%q err=%v", artID, abs, err)
	}
}

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

func TestRehydrateFromMessages_screenCapture(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "capture.jpg")
	if err := os.WriteFile(imgPath, []byte{0xff, 0xd8, 0xff, 0xd9}, 0o600); err != nil {
		t.Fatal(err)
	}
	reg, err := NewRegistry("sess-screen", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	messages := []llm.Message{{
		Role:       "tool",
		ToolCallID: "call-screen",
		Name:       "screen_capture",
		Content:    fmt.Sprintf(`{"ok":true,"screenshot_path":%q}`, imgPath),
	}}
	out := RehydrateFromMessages(reg, messages, nil)
	items := out["call-screen"]
	if len(items) != 1 || items[0]["url"] == "" || items[0]["label"] != "screen_capture" {
		t.Fatalf("media=%v", out)
	}
}
