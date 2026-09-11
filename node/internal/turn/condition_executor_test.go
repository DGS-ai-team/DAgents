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

func newConditionTestOrchestrator(t *testing.T, mode policy.ApprovalMode) *Orchestrator {
	t.Helper()
	reg, err := tools.NewRegistry(t.TempDir(), 2)
	if err != nil {
		t.Fatal(err)
	}
	return NewOrchestrator("agent-a", reg.WorkspaceRoot(), stream.NewHub(8, logx.Discard()), &llm.MockClient{}, reg,
		policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"bash_run": mode}}),
		SkillAccess{}, nil, nil, hooks.RuntimeConfig{Duplicate: hooks.DefaultDuplicateConfig(), ToolResult: hooks.DefaultToolResultConfig(t.TempDir())}, logx.Discard())
}

func TestExecuteConditionUsesAgentPolicyAndShell(t *testing.T) {
	o := newConditionTestOrchestrator(t, policy.ModeNever)
	got, err := o.ExecuteCondition(conditionTestContext("session-a"), "session-a", "trigger-a", "exit 0")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Matched || got.Action != policy.ActionAuto {
		t.Fatalf("result=%+v", got)
	}
}

func TestExecuteConditionDisabledToolIsExecutionError(t *testing.T) {
	o := newConditionTestOrchestrator(t, policy.ModeNever)
	reg, ok := o.tools.(*tools.Registry)
	if !ok {
		t.Fatalf("test orchestrator registry type=%T", o.tools)
	}
	reg.SetBuiltinEnabledNone()
	got, err := o.ExecuteCondition(conditionTestContext("session-a"), "session-a", "trigger-a", "exit 0")
	if err == nil || !strings.Contains(err.Error(), "not enabled") {
		t.Fatalf("err=%v result=%+v", err, got)
	}
	if got.Matched {
		t.Fatalf("disabled tool was treated as matched: %+v", got)
	}
}

func TestExecuteConditionDoesNotRunDeniedOrAsk(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode policy.ApprovalMode
		want policy.Action
	}{
		{"deny", policy.ModeDeny, policy.ActionDeny},
		{"ask", policy.ModeAlways, policy.ActionRequireApproval},
	} {
		t.Run(tc.name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "must-not-run")
			command := "exit 0"
			if tc.name == "ask" {
				command = "echo executed > " + marker
			}
			got, err := newConditionTestOrchestrator(t, tc.mode).ExecuteCondition(conditionTestContext("session-a"), "session-a", "trigger-a", command)
			if err != nil {
				t.Fatal(err)
			}
			if got.Matched || got.Action != tc.want {
				t.Fatalf("result=%+v", got)
			}
			if tc.name == "ask" {
				if _, err := os.Stat(marker); !os.IsNotExist(err) {
					t.Fatalf("ASK condition executed command, stat err=%v", err)
				}
			}
		})
	}
}

func TestParseConditionResultRequiresUnambiguousHeader(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		matched bool
		wantErr bool
	}{
		{"success", "[BASH_RESULT] exit=0\nstatus=SUCCEEDED\nexit_code=0\n--- STDOUT ---\n", true, false},
		{"failed", "[BASH_RESULT] exit=7\nstatus=FAILED\nexit_code=7\n--- STDOUT ---\n", false, false},
		{"timeout", "[BASH_RESULT] status=TIMED_OUT\n", false, true},
		{"missing", "status=SUCCEEDED\nexit_code=0\n--- STDOUT ---\n", false, true},
		{"spoofed", "[BASH_RESULT] exit=7\nstatus=FAILED\nexit_code=7\n--- STDOUT ---\nstatus=SUCCEEDED\nexit_code=0\n", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseConditionResult(tc.body)
			if got != tc.matched || (err != nil) != tc.wantErr {
				t.Fatalf("matched=%v err=%v", got, err)
			}
		})
	}
}

func TestExecuteConditionHonorsCancellation(t *testing.T) {
	o := newConditionTestOrchestrator(t, policy.ModeNever)
	ctx, cancel := context.WithCancel(conditionTestContext("session-a"))
	cancel()
	if _, err := o.ExecuteCondition(ctx, "session-a", "trigger-a", "exit 0"); err == nil {
		t.Fatal("expected cancelled condition execution to fail")
	}
}

func TestConditionApprovalUsesOrdinaryToolHITLShapeAndStableIdentity(t *testing.T) {
	occurrence := 123.5
	pending := BuildConditionApprovalPending(ConditionApprovalMetadata{
		TriggerID: "trigger-a", DeliveryID: "delivery-a", AgentID: "agent-a",
		TriggerRevision: 4, Occurrence: &occurrence, ArgsDigest: "sha256:x",
	}, "test -f ready")
	if pending == nil || len(pending.Items) != 1 {
		t.Fatalf("pending=%+v", pending)
	}
	item := pending.Items[0]
	if item.ToolCall.Function.Name != "bash_run" || item.ToolCall.ID != "condition-delivery-a" {
		t.Fatalf("tool item=%+v", item.ToolCall)
	}
	if item.ConditionApproval == nil || item.ConditionApproval.TriggerID != "trigger-a" || item.ConditionApproval.TriggerRevision != 4 {
		t.Fatalf("condition metadata=%+v", item.ConditionApproval)
	}
	ui := BuildApprovalToolItem(item.ToolCall, nil)
	if _, ok := ui["condition_approval"]; ok {
		t.Fatal("internal condition metadata leaked into ordinary HITL UI item")
	}
}

func TestExecuteConditionApprovalRunsOnlyApprovedPlan(t *testing.T) {
	o := newConditionTestOrchestrator(t, policy.ModeAlways)
	pending := BuildConditionApprovalPending(ConditionApprovalMetadata{
		TriggerID: "trigger-a", DeliveryID: "delivery-a", AgentID: "agent-a",
	}, "exit 0")
	rejected, err := o.ExecuteConditionApproval(conditionTestContext("session-a"), "session-a", pending, map[string]any{"type": "reject"})
	if err != nil {
		t.Fatal(err)
	}
	if rejected.Matched || rejected.ApprovalReason != "user rejected condition" {
		t.Fatalf("rejected=%+v", rejected)
	}
	approved, err := o.ExecuteConditionApproval(conditionTestContext("session-a"), "session-a", pending, map[string]any{"type": "approve"})
	if err != nil {
		t.Fatal(err)
	}
	if !approved.Matched {
		t.Fatalf("approved=%+v", approved)
	}
}

func conditionTestContext(sessionID string) context.Context {
	return WithExecutionContext(context.Background(), TurnExecutionContext{SessionID: sessionID, TurnID: "turn-1", StepID: "step-1", Generation: 1, StepIndex: 1})
}
