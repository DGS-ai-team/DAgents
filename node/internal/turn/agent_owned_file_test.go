package turn

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

func writeAgentOwnedPolicyDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeTurnPolicyFile(t, dir, "tool.approval.txt", "write_file=rule\nsearch_replace=rule\nread_file=never\n")
	writeTurnPolicyFile(t, dir, "shell/bash.approval.txt", "")
	writeTurnPolicyFile(t, dir, "shell/cmd.approval.txt", "")
	writeTurnPolicyFile(t, dir, "shell/powershell.approval.txt", "")
	return dir
}

func testRegistryAt(t *testing.T, root string) *tools.Registry {
	t.Helper()
	reg, err := tools.NewRegistry(root, 30)
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func newAgentOwnedOrchestrator(t *testing.T, root string) *Orchestrator {
	t.Helper()
	dir := writeAgentOwnedPolicyDir(t)
	engine, err := policy.LoadFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	reg := testRegistryAt(t, root)
	disabled := false
	hub := stream.NewHub(8, logx.Discard())
	return NewOrchestrator("a1", root, hub, &llm.MockClient{}, reg, engine, SkillAccess{}, nil, nil, hooks.RuntimeConfig{
		Duplicate:      hooks.DuplicateConfig{Enabled: &disabled},
		ToolResult:     hooks.DefaultToolResultConfig(root),
		AgentOwnedFile: hooks.DefaultAgentOwnedFileConfig(),
	}, logx.Discard())
}

func TestAgentOwnedFileTrustChain_autoAfterCreate(t *testing.T) {
	root := t.TempDir()
	orch := newAgentOwnedOrchestrator(t, root)
	ctx := context.Background()
	sessionID := "sess-trust"

	var history []llm.Message
	createCall := llm.ToolCall{
		ID:   "call-create",
		Type: "function",
		Function: llm.ToolCallFunction{
			Name:      "write_file",
			Arguments: `{"path":"draft.txt","content":"v1","call_purpose":"create"}`,
		},
	}
	history = append(history, llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{createCall}})
	pending, state, err := orch.processToolCalls(ctx, sessionID, &history, []llm.ToolCall{createCall})
	if err != nil {
		t.Fatal(err)
	}
	if state != "awaiting_hitl" || pending == nil {
		t.Fatalf("first create state=%q pending=%v", state, pending)
	}

	continueResumeAndDrain(t, orch, ctx, sessionID, &history, map[string]any{"type": "approve"}, pending, 0)
	if len(history) == 0 {
		t.Fatal("expected tool history after approve")
	}

	editCall := llm.ToolCall{
		ID:   "call-edit",
		Type: "function",
		Function: llm.ToolCallFunction{
			Name:      "search_replace",
			Arguments: `{"path":"draft.txt","old_string":"v1","new_string":"v2","call_purpose":"edit"}`,
		},
	}
	history = append(history, llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{editCall}})
	pending2, state2, err := orch.processToolCalls(ctx, sessionID, &history, []llm.ToolCall{editCall})
	if err != nil {
		t.Fatal(err)
	}
	if pending2 != nil || state2 == "awaiting_hitl" {
		t.Fatalf("trusted edit should auto: state=%q pending=%v", state2, pending2)
	}
	if len(history) < 2 {
		t.Fatalf("history = %+v", history)
	}
}

func TestAgentOwnedFileTrustChain_existingFileAlwaysApproves(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "readme.txt")
	if err := os.WriteFile(existing, []byte("upstream"), 0o644); err != nil {
		t.Fatal(err)
	}
	orch := newAgentOwnedOrchestrator(t, root)
	ctx := context.Background()

	var history []llm.Message
	createCall := llm.ToolCall{
		ID: "c1", Type: "function",
		Function: llm.ToolCallFunction{Name: "write_file", Arguments: `{"path":"readme.txt","content":"v2","call_purpose":"t"}`},
	}
	history = append(history, llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{createCall}})
	pending, state, err := orch.processToolCalls(ctx, "sess-1", &history, []llm.ToolCall{createCall})
	if err != nil || state != "awaiting_hitl" {
		t.Fatalf("overwrite existing: state=%q err=%v", state, err)
	}
	continueResumeAndDrain(t, orch, ctx, "sess-1", &history, map[string]any{"type": "approve"}, pending, 0)

	editCall := llm.ToolCall{
		ID: "c2", Type: "function",
		Function: llm.ToolCallFunction{Name: "write_file", Arguments: `{"path":"readme.txt","content":"v3","call_purpose":"t"}`},
	}
	history = append(history, llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{editCall}})
	pending2, state2, err := orch.processToolCalls(ctx, "sess-1", &history, []llm.ToolCall{editCall})
	if err != nil {
		t.Fatal(err)
	}
	if pending2 == nil || state2 != "awaiting_hitl" {
		t.Fatalf("non-owned overwrite should re-approve: state=%q pending=%v", state2, pending2)
	}
}

func TestAgentOwnedFileTrustChain_externalMtimeChange(t *testing.T) {
	root := t.TempDir()
	orch := newAgentOwnedOrchestrator(t, root)
	ctx := context.Background()
	sessionID := "sess-mtime"

	var history []llm.Message
	createCall := llm.ToolCall{
		ID: "c1", Type: "function",
		Function: llm.ToolCallFunction{Name: "write_file", Arguments: `{"path":"note.txt","content":"a","call_purpose":"t"}`},
	}
	history = append(history, llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{createCall}})
	pending, _, err := orch.processToolCalls(ctx, sessionID, &history, []llm.ToolCall{createCall})
	if err != nil {
		t.Fatal(err)
	}
	continueResumeAndDrain(t, orch, ctx, sessionID, &history, map[string]any{"type": "approve"}, pending, 0)

	path := filepath.Join(root, "note.txt")
	if err := os.Chtimes(path, time.Now(), time.Now().Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}

	editCall := llm.ToolCall{
		ID: "c2", Type: "function",
		Function: llm.ToolCallFunction{Name: "search_replace", Arguments: `{"path":"note.txt","old_string":"a","new_string":"b","call_purpose":"t"}`},
	}
	history = append(history, llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{editCall}})
	pending2, state2, err := orch.processToolCalls(ctx, sessionID, &history, []llm.ToolCall{editCall})
	if err != nil {
		t.Fatal(err)
	}
	if pending2 == nil || state2 != "awaiting_hitl" {
		t.Fatalf("external mtime change should require approval: state=%q pending=%v", state2, pending2)
	}
}
