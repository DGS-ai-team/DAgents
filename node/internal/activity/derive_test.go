package activity

import (
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func TestDeriveFromMessages_filesAndCommands(t *testing.T) {
	msgs := []llm.Message{
		{
			Role: "assistant",
			ToolCalls: []llm.ToolCall{
				{ID: "c1", Function: llm.ToolCallFunction{Name: "write_file", Arguments: `{"path":"a.txt","content":"hi"}`}},
				{ID: "c2", Function: llm.ToolCallFunction{Name: "bash_run", Arguments: `{"command":"ls -la"}`}},
			},
		},
		{Role: "tool", ToolCallID: "c1", Name: "write_file", Content: "wrote 2 bytes to a.txt (encoding=utf-8)"},
		{Role: "tool", ToolCallID: "c2", Name: "bash_run", Content: "total 0\n"},
		{
			Role: "assistant",
			ToolCalls: []llm.ToolCall{
				{ID: "c3", Function: llm.ToolCallFunction{Name: "search_replace", Arguments: `{"path":"a.txt","old_string":"hi","new_string":"hello"}`}},
			},
		},
		{Role: "tool", ToolCallID: "c3", Name: "search_replace", Content: "replaced 1 occurrence in a.txt"},
	}
	snap := DeriveFromMessages(msgs)
	if snap.FileCount != 1 || len(snap.Files) != 1 {
		t.Fatalf("files=%+v", snap.Files)
	}
	f := snap.Files[0]
	if f.Path != "a.txt" {
		t.Fatalf("path=%q", f.Path)
	}
	if len(f.Ops) != 2 {
		t.Fatalf("ops=%v", f.Ops)
	}
	if snap.CmdCount != 1 || snap.Commands[0].Command != "ls -la" {
		t.Fatalf("commands=%+v", snap.Commands)
	}
	if snap.Commands[0].Status != "ok" {
		t.Fatalf("status=%s", snap.Commands[0].Status)
	}
}

func TestDeriveFromMessages_empty(t *testing.T) {
	snap := DeriveFromMessages(nil)
	if snap.FileCount != 0 || snap.CmdCount != 0 {
		t.Fatalf("%+v", snap)
	}
}
