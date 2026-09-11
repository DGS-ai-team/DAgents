package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

type conditionCountingClient struct{ calls atomic.Int32 }

func (c *conditionCountingClient) StreamChat(context.Context, llm.ChatRequest, llm.StreamHandler) (llm.ChatResult, error) {
	c.calls.Add(1)
	return llm.ChatResult{}, fmt.Errorf("unexpected model call")
}
func (c *conditionCountingClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	c.calls.Add(1)
	return "", fmt.Errorf("unexpected model call")
}
func (c *conditionCountingClient) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func newConditionLifecycleRuntime(t *testing.T, st *store.SQLiteStore, sessionID string) *runtime {
	t.Helper()
	workspace := t.TempDir()
	registry, err := tools.NewRegistry(workspace, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.SetBuiltinEnabled(config.ExpandBuiltinToolGroups([]string{"filesystem"})); err != nil {
		t.Fatal(err)
	}
	policyEngine := policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{
		"bash_run": policy.ModeNever,
	}})
	hub := stream.NewHub(32, logx.Discard())
	return newRuntimeWithPublisher(sessionID, "agent-1", hub, hub, &llm.MockClient{}, registry,
		policyEngine, st, logx.Discard(), nil, nil, nil, false, 0, 0,
		TurnOptions{WorkspaceRoot: workspace, SkillsEnabled: false}, nil)
}

func conditionRequest(id string) ConditionApprovalRequest {
	return ConditionApprovalRequest{Metadata: turn.ConditionApprovalMetadata{
		TriggerID: "trigger-1", DeliveryID: id, AgentID: "agent-1", TriggerRevision: 1,
	}, Command: "echo condition"}
}

func TestManagerExecuteConditionUsesAgentGateAndNeverCallsModel(t *testing.T) {
	workspace := t.TempDir()
	registry, err := tools.NewRegistry(workspace, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.SetBuiltinEnabled(config.ExpandBuiltinToolGroups([]string{"filesystem"})); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "manager-condition.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	client := &conditionCountingClient{}
	mgr := NewManager("agent-1", stream.NewHub(32, logx.Discard()), client, registry,
		policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"bash_run": policy.ModeNever}}),
		st, TurnOptions{AutoAgent: true}, logx.Discard())
	defer mgr.Stop()
	rt, _, err := mgr.CreateWithOptionsAndLLM("condition-manager", TurnOptions{AutoAgent: true, WorkspaceRoot: workspace}, registry, nil, client, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	result, err := mgr.ExecuteCondition(context.Background(), triggers.ConditionRequest{TriggerID: "trigger-manager", DeliveryID: "delivery-manager", SessionID: rt.ID, AgentID: "agent-1", Revision: 1, Command: "touch marker; echo ready"})
	if err != nil || result.Status != triggers.ConditionMatched {
		t.Fatalf("manager condition result=%+v err=%v", result, err)
	}
	if client.calls.Load() != 0 {
		t.Fatalf("condition invoked model %d times", client.calls.Load())
	}
	if _, err := os.Stat(filepath.Join(workspace, "marker")); err != nil {
		t.Fatalf("condition marker missing: %v", err)
	}
}

func TestConditionApprovalLifecyclePersistsExecutionFenceAndSupportsReject(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "turn-events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	r := newConditionLifecycleRuntime(t, st, "condition-approve")
	if err := r.requestConditionApproval(conditionRequest("delivery-approve")); err != nil {
		t.Fatal(err)
	}
	pending := r.pendingSnapshot()
	if pending == nil || len(pending.Items) != 1 {
		t.Fatalf("pending approval = %#v", pending)
	}
	if got, want := pending.Items[0].ConditionApproval.ArgsDigest, turn.Digest(pending.Items[0].ToolCall.Function.Arguments); got != want {
		t.Fatalf("condition approval args digest = %q, want %q", got, want)
	}
	r.handleConditionResume(context.Background(), map[string]any{"type": "approve"}, pending)

	snapshot := r.turnCoordinator.Snapshot()
	if snapshot.HasActiveTurn || snapshot.TurnStatus != turn.TurnStatusCompleted {
		t.Fatalf("approval terminal state = %+v", snapshot)
	}
	events, err := st.ListTurnEventsForTurn(context.Background(), r.session.ID, snapshot.TurnID)
	if err != nil {
		t.Fatal(err)
	}
	started, completed, modelRequests := -1, -1, 0
	for i, event := range events {
		switch event.EventType {
		case turn.EventToolExecutionStarted:
			started = i
		case turn.EventToolExecutionCompleted:
			completed = i
		case turn.EventModelRequestStarted:
			modelRequests++
		}
	}
	if started < 0 || completed <= started {
		t.Fatalf("execution lifecycle ordering started=%d completed=%d", started, completed)
	}
	if modelRequests != 0 {
		t.Fatalf("condition approval unexpectedly called model %d times", modelRequests)
	}
	// A second resume cannot recreate the consumed interaction or execute the
	// command again.
	r.handleConditionResume(context.Background(), map[string]any{"type": "approve"}, pending)
	if after := r.turnCoordinator.Snapshot(); after.TurnID != snapshot.TurnID || after.HasActiveTurn {
		t.Fatalf("duplicate resume changed terminal projection = %+v", after)
	}

	malformed := newConditionLifecycleRuntime(t, st, "condition-malformed")
	if err := malformed.requestConditionApproval(conditionRequest("delivery-malformed")); err != nil {
		t.Fatal(err)
	}
	malformed.handleConditionResume(context.Background(), map[string]any{"unexpected": true}, malformed.pendingSnapshot())
	if pending := malformed.pendingSnapshot(); pending == nil || len(pending.Items) != 1 {
		t.Fatalf("malformed resume consumed approval: %#v", pending)
	}

	reject := newConditionLifecycleRuntime(t, st, "condition-reject")
	if err := reject.requestConditionApproval(conditionRequest("delivery-reject")); err != nil {
		t.Fatal(err)
	}
	reject.handleConditionResume(context.Background(), map[string]any{"type": "reject"}, reject.pendingSnapshot())
	rejectState := reject.turnCoordinator.Snapshot()
	if rejectState.HasActiveTurn || rejectState.TurnStatus != turn.TurnStatusCompleted {
		t.Fatalf("reject terminal state = %+v", rejectState)
	}
	rejectEvents, err := st.ListTurnEventsForTurn(context.Background(), reject.session.ID, rejectState.TurnID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range rejectEvents {
		if event.EventType == turn.EventModelRequestStarted {
			t.Fatal("rejected condition unexpectedly called model")
		}
	}
}

func TestConditionApprovalRestartDoesNotReplayPendingOrFencedExecution(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "restart.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	first := newConditionLifecycleRuntime(t, st, "condition-pending")
	if err := first.requestConditionApproval(conditionRequest("delivery-pending")); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	second := newConditionLifecycleRuntime(t, reopened, "condition-pending")
	second.restoreLifecycleEvents()
	if pending := second.pendingSnapshot(); pending == nil || len(pending.Items) != 1 {
		t.Fatalf("restored pending approval = %#v", pending)
	}
	if state := second.turnCoordinator.Snapshot(); state.RecoveryRequired {
		t.Fatalf("ordinary pending approval incorrectly marked recovery: %+v", state)
	}

	// Simulate a process dying after the durable pre-execution fence. The
	// restored projection must mark the command unknown and must not run bash.
	if err := second.lifecyclePrepareResume(map[string]any{"type": "approve"}); err != nil {
		t.Fatal(err)
	}
	state := second.turnCoordinator.Snapshot()
	if _, err := second.lifecycleDispatchErr(turn.TurnCommand{Type: turn.CommandToolExecutionStarted,
		SessionID: state.SessionID, TurnID: state.TurnID, StepID: state.StepID,
		Generation: state.Generation, ToolCallID: "condition-delivery-pending",
		ToolExecutionID: "condition-delivery-pending-execution", At: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	finalStore, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer finalStore.Close()
	recovered := newConditionLifecycleRuntime(t, finalStore, "condition-pending")
	recovered.restoreLifecycleEvents()
	recoveredState := recovered.turnCoordinator.Snapshot()
	if !recoveredState.RecoveryRequired {
		t.Fatalf("fenced execution was not marked recovery: %+v", recoveredState)
	}
	if status, ok := recovered.turnCoordinator.ToolExecutionStatusForCall("condition-delivery-pending"); !ok || status != turn.ToolExecutionStatusUnknown {
		t.Fatalf("fenced execution status = %s/%v", status, ok)
	}
}
