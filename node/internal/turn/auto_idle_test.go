package turn

import (
	"context"
	"os"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

func autoIdleCall(id string) llm.ToolCall {
	return llm.ToolCall{ID: id, Type: "function", Function: llm.ToolCallFunction{Name: "auto_idle", Arguments: `{}`}}
}

func TestAutoIdleRequiresSuccessfulReadOnlyWork(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(root+string(os.PathSeparator)+"ok.txt", []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := testRegistryAt(t, root)
	if err := reg.SetBuiltinEnabled([]string{"read_file", "auto_idle"}); err != nil {
		t.Fatal(err)
	}
	orch := NewOrchestrator("a1", root, stream.NewHub(8, logx.Discard()), &llm.MockClient{}, reg,
		policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"read_file": policy.ModeNever, "auto_idle": policy.ModeNever}}),
		SkillAccess{}, nil, nil, hooks.RuntimeConfig{Duplicate: hooks.DefaultDuplicateConfig(), ToolResult: hooks.DefaultToolResultConfig(root)}, logx.Discard())
	trusted := tools.WithTrustedAutoIdleActivation(context.Background(), "a1", "auto", "delivery")
	orch.BeginTrustedAutoIdleActivation("ok")
	var okHistory []llm.Message
	if _, _, err := orch.processToolCalls(trusted, "ok", &okHistory, []llm.ToolCall{{ID: "read-ok", Type: "function", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"ok.txt"}`}}}); err != nil {
		t.Fatalf("successful read failed: %v", err)
	}
	if _, reason, err := orch.processToolCalls(trusted, "ok", &okHistory, []llm.ToolCall{autoIdleCall("idle-ok")}); err != nil || reason != "no_work" {
		t.Fatalf("successful read did not permit auto_idle: reason=%q err=%v", reason, err)
	}
	orch.BeginTrustedAutoIdleActivation("failed")
	// A missing read is an execution failure and must fence auto_idle.
	var failedHistory []llm.Message
	if _, _, err := orch.processToolCalls(trusted, "failed", &failedHistory, []llm.ToolCall{{ID: "missing", Type: "function", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"missing.txt"}`}}}); err != nil {
		t.Fatalf("failed read unexpectedly aborted tool batch: %v", err)
	}
	if _, _, err := orch.processToolCalls(trusted, "failed", &failedHistory, []llm.ToolCall{autoIdleCall("idle-failed")}); err == nil {
		t.Fatal("auto_idle succeeded after failed read")
	}
}
