package turn

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

func TestResumeApprovedToolStillExecutesWhenPolicyRequiresApproval(t *testing.T) {
	root := t.TempDir()
	reg := testRegistryAt(t, root)
	orch := NewOrchestrator("agent-a", root, stream.NewHub(8, logx.Discard()), &llm.MockClient{}, reg,
		policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"write_file": policy.ModeAlways}}),
		SkillAccess{}, nil, nil, hooks.RuntimeConfig{Duplicate: hooks.DuplicateConfig{Enabled: boolPtr(false)}, ToolResult: hooks.DefaultToolResultConfig(root)}, logx.Discard())
	call := llm.ToolCall{ID: "call-write", Type: "function", Function: llm.ToolCallFunction{Name: "write_file", Arguments: `{"path":"revoked.txt","content":"must not land"}`}}
	history := []llm.Message{{Role: "assistant", ToolCalls: []llm.ToolCall{call}}}
	pending, state, err := orch.processToolCalls(context.Background(), "session-a", &history, []llm.ToolCall{call})
	if err != nil || state != "awaiting_hitl" || pending == nil {
		t.Fatalf("pending=%+v state=%s err=%v", pending, state, err)
	}
	orch.SetPolicy(policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"write_file": policy.ModeAlways}}))
	outcome := orch.ContinueAfterResume(context.Background(), "session-a", &history, map[string]any{"type": "approve"}, pending)
	if outcome.Err != nil || !outcome.ScheduleToolResult {
		t.Fatalf("explicit approval was not honored: %+v", outcome)
	}
	if _, err := os.Stat(filepath.Join(root, "revoked.txt")); err != nil {
		t.Fatalf("approved write did not land: %v", err)
	}
}

func TestResumeApprovedToolIsRejectedWhenPolicyDenies(t *testing.T) {
	root := t.TempDir()
	reg := testRegistryAt(t, root)
	orch := NewOrchestrator("agent-a", root, stream.NewHub(8, logx.Discard()), &llm.MockClient{}, reg, policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"write_file": policy.ModeAlways}}), SkillAccess{}, nil, nil, hooks.RuntimeConfig{Duplicate: hooks.DuplicateConfig{Enabled: boolPtr(false)}, ToolResult: hooks.DefaultToolResultConfig(root)}, logx.Discard())
	call := llm.ToolCall{ID: "call-deny", Type: "function", Function: llm.ToolCallFunction{Name: "write_file", Arguments: `{"path":"denied.txt","content":"no"}`}}
	history := []llm.Message{{Role: "assistant", ToolCalls: []llm.ToolCall{call}}}
	pending, _, _ := orch.processToolCalls(context.Background(), "session-a", &history, []llm.ToolCall{call})
	orch.SetPolicy(policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"write_file": policy.ModeDeny}}))
	outcome := orch.ContinueAfterResume(context.Background(), "session-a", &history, map[string]any{"type": "approve"}, pending)
	if outcome.Err != nil || !outcome.ScheduleToolResult || len(history) < 2 || !strings.Contains(history[len(history)-1].Content, "policy_denied") {
		t.Fatalf("deny was not honored: %+v history=%+v", outcome, history)
	}
	if _, err := os.Stat(filepath.Join(root, "denied.txt")); !os.IsNotExist(err) {
		t.Fatalf("denied write landed: %v", err)
	}
}

func TestQueuedAutoToolRevokeEntersApprovalBeforeExecution(t *testing.T) {
	root := t.TempDir()
	reg := testRegistryAt(t, root)
	revoked := false
	grant := policy.Grant{ID: "g", Tools: []string{"write_file"}, Workspace: root, ExpiresAt: time.Now().Add(time.Hour)}
	allow := policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"write_file": policy.ModeRule}, Grants: []policy.Grant{grant}})
	orch := NewOrchestrator("agent-a", root, stream.NewHub(8, logx.Discard()), &llm.MockClient{}, reg, allow, SkillAccess{}, nil, nil, hooks.RuntimeConfig{Duplicate: hooks.DuplicateConfig{Enabled: boolPtr(false)}, ToolResult: hooks.DefaultToolResultConfig(root)}, logx.Discard())
	raw, _ := json.Marshal(map[string]any{"path": filepath.Join(root, "queued.txt"), "content": "no"})
	call := llm.ToolCall{ID: "call-auto", Type: "function", Function: llm.ToolCallFunction{Name: "write_file", Arguments: string(raw)}}
	if got := allow.DecideTool("write_file", map[string]any{"path": filepath.Join(root, "queued.txt")}); got != policy.ActionAuto {
		t.Fatalf("grant was not auto before revoke: %s", got)
	}
	orch.executionGuard = executionGuardFunc(func(ctx context.Context, sessionID string, history *[]llm.Message, tc llm.ToolCall) hooks.ToolBeforeEachResult {
		if !revoked {
			revoked = true
			orch.SetPolicy(policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"write_file": policy.ModeRule}}))
		}
		return orch.evaluateToolBeforeEach(ctx, sessionID, history, tc)
	})
	history := []llm.Message{{Role: "assistant", ToolCalls: []llm.ToolCall{call}}}
	pending, state, err := orch.processToolCalls(context.Background(), "session-a", &history, []llm.ToolCall{call})
	if err != nil || state != "awaiting_hitl" || pending == nil {
		t.Fatalf("revoked auto call did not enter approval: state=%s pending=%+v err=%v", state, pending, err)
	}
	if _, err := os.Stat(filepath.Join(root, "queued.txt")); !os.IsNotExist(err) {
		t.Fatalf("queued write landed: %v", err)
	}
}

func boolPtr(v bool) *bool { return &v }
