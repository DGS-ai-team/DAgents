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
)

func writeTurnTestPolicyDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeTurnPolicyFile(t, dir, "tool.approval.txt", "read_file=never\n")
	writeTurnPolicyFile(t, dir, "shell/bash.approval.txt", "")
	writeTurnPolicyFile(t, dir, "shell/cmd.approval.txt", "")
	writeTurnPolicyFile(t, dir, "shell/powershell.approval.txt", "")
	return dir
}

func writeTurnPolicyFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDuplicateToolCallTriggersStandardApproval(t *testing.T) {
	dir := writeTurnTestPolicyDir(t)
	engine, err := policy.LoadFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	hub := stream.NewHub(8, logx.Discard())
	reg := testRegistry(t)
	orch := NewOrchestrator("a1", t.TempDir(), hub, &llm.MockClient{}, reg, engine, SkillAccess{}, DefaultMaxToolLoops(), nil, nil, hooks.DefaultDuplicateConfig(), logx.Discard())

	raw := `{"job_id":"j1","call_purpose":"poll"}`
	fp := hooks.ToolArgsFingerprint("background_job_status", raw)
	orch.toolExecLog.RecordSuccess("background_job_status", fp, "call-prev", "status=RUNNING")

	var history []llm.Message
	pending, state, err := orch.processToolCalls(context.Background(), "sess-1", &history, []llm.ToolCall{{
		ID:   "call-dup",
		Type: "function",
		Function: llm.ToolCallFunction{
			Name:      "background_job_status",
			Arguments: raw,
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if state != "awaiting_tool_approval" {
		t.Fatalf("state = %q", state)
	}
	if pending == nil || len(pending.ToolCalls) != 1 {
		t.Fatalf("pending = %+v", pending)
	}

	item := buildApprovalToolItem(pending.ToolCalls[0], &hooks.DuplicateMeta{
		WindowSeconds:        60,
		PreviousToolCallID:   "call-prev",
		SecondsSincePrevious: 12,
		ResultPreview:        "status=RUNNING",
	})
	reason, _ := item["approval_reason"].(string)
	if !strings.Contains(reason, "【重复调用】") {
		t.Fatalf("approval_reason = %q", reason)
	}
}

func TestDuplicateToolCallResumeUsesStandardApproval(t *testing.T) {
	dir := writeTurnTestPolicyDir(t)
	engine, err := policy.LoadFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	hub := stream.NewHub(8, logx.Discard())
	reg := testRegistry(t)
	orch := NewOrchestrator("a1", t.TempDir(), hub, &llm.MockClient{}, reg, engine, SkillAccess{}, DefaultMaxToolLoops(), nil, nil, hooks.DefaultDuplicateConfig(), logx.Discard())

	raw := `{"job_id":"j1"}`
	fp := hooks.ToolArgsFingerprint("background_job_status", raw)
	orch.toolExecLog.RecordSuccess("background_job_status", fp, "call-prev", "ok")

	var history []llm.Message
	pending, _, err := orch.processToolCalls(context.Background(), "sess-1", &history, []llm.ToolCall{{
		ID:   "call-dup",
		Type: "function",
		Function: llm.ToolCallFunction{
			Name:      "background_job_status",
			Arguments: raw,
		},
	}})
	if err != nil {
		t.Fatal(err)
	}

	outcome := orch.ContinueAfterResume(context.Background(), "sess-1", &history, map[string]any{
		"type": "approve",
	}, pending, nil, 0)
	if outcome.Err != nil {
		t.Fatal(outcome.Err)
	}
	if !outcome.ScheduleToolResult {
		t.Fatal("expected schedule tool result")
	}
	if len(history) != 1 || history[0].Role != "tool" {
		t.Fatalf("history = %+v", history)
	}
}
