package shared

import (
	"strings"
	"testing"
)

func TestTranscriptTail(t *testing.T) {
	tr := NewTranscript(5)
	for i := 1; i <= 7; i++ {
		tr.Add(string(rune('0' + i)))
	}
	if got := len(tr.Tail(0)); got != 5 {
		t.Fatalf("cap tail len = %d", got)
	}
	tail := tr.Tail(2)
	if len(tail) != 2 || tail[0] != "6" || tail[1] != "7" {
		t.Fatalf("tail = %v", tail)
	}
}

func TestToolFoldFormatNestedToolCall(t *testing.T) {
	f := &ToolFold{}
	line := f.Format("tool_call", map[string]any{
		"tool_calls": []any{
			map[string]any{
				"id": "call-1",
				"function": map[string]any{
					"name":      "trigger_create",
					"arguments": `{"name":"喝水提醒"}`,
				},
			},
		},
	})
	if !strings.Contains(line, "trigger_create") || !strings.Contains(line, "喝水提醒") {
		t.Fatalf("line = %q", line)
	}
	if strings.Contains(line, "<nil>") {
		t.Fatalf("line = %q", line)
	}
}

func TestToolFoldFormat(t *testing.T) {
	f := &ToolFold{}
	line := f.Format("tool_call", map[string]any{"name": "read_file", "content": "hello world"})
	if line == "" {
		t.Fatal("empty format")
	}
	f.SetVerbose(true)
	verbose := f.Format("tool_result", map[string]any{"name": "read_file"})
	if verbose == "" {
		t.Fatal("empty verbose")
	}
}
