package session

import (
	"context"
	"fmt"
	goruntime "runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func TestHydrateProjectsActiveToolCallUntilTerminal(t *testing.T) {
	now := time.Now().UTC()
	r := newLifecycleTestRuntime()
	r.queue = queue.NewMessageQueue()
	c := r.turnCoordinator
	commands := []turn.TurnCommand{
		{Type: turn.CommandStartTurn, SessionID: r.session.ID, TurnID: "turn-hydrate", Generation: 1, Source: turn.TurnSourceHuman, At: now},
		{Type: turn.CommandStartStep, SessionID: r.session.ID, TurnID: "turn-hydrate", StepID: "step-hydrate", Generation: 1, At: now},
		{Type: turn.CommandAssistantReceived, SessionID: r.session.ID, TurnID: "turn-hydrate", StepID: "step-hydrate", Generation: 1, HasTools: true, At: now},
		{Type: turn.CommandToolCallRecorded, SessionID: r.session.ID, TurnID: "turn-hydrate", StepID: "step-hydrate", Generation: 1, ToolCallID: "call-hydrate", ToolName: "bash_run", Arguments: []byte(`{"command":"sleep 30"}`), At: now},
	}
	for _, command := range commands {
		if _, err := c.Dispatch(command); err != nil {
			t.Fatalf("dispatch %s: %v", command.Type, err)
		}
	}
	state := c.Snapshot()
	if state.StepStatus != turn.StepStatusExecutingTools || len(state.ActiveToolCalls) != 1 {
		t.Fatalf("proposed call projection = %+v", state)
	}
	manager := NewManager("agent-1", nil, nil, nil, nil, nil, TurnOptions{}, logx.Discard())
	manager.sessions[r.session.ID] = r
	view, err := manager.GetHydrateView(r.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertSingleProjectedCall(t, view, "call-hydrate", "proposed")

	if _, err := c.Dispatch(turn.TurnCommand{Type: turn.CommandToolExecutionStarted, SessionID: r.session.ID, TurnID: "turn-hydrate", StepID: "step-hydrate", Generation: 1, ToolExecutionID: "call-hydrate-execution", At: now}); err != nil {
		t.Fatal(err)
	}
	view, err = manager.GetHydrateView(r.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertSingleProjectedCall(t, view, "call-hydrate", "running")

	if _, err := c.Dispatch(turn.TurnCommand{Type: turn.CommandToolExecutionCompleted, SessionID: r.session.ID, TurnID: "turn-hydrate", StepID: "step-hydrate", Generation: 1, ToolExecutionID: "call-hydrate-execution", ExecutionStatus: turn.ToolExecutionStatusSucceeded, At: now}); err != nil {
		t.Fatal(err)
	}
	view, err = manager.GetHydrateView(r.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertSingleProjectedCall(t, view, "call-hydrate", "succeeded")

	// Once the runtime commits the assistant/tool messages, hydrate should use
	// that durable ordering and not append a second projected call.
	r.messages = []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call-hydrate", Type: "function", Function: llm.ToolCallFunction{Name: "bash_run", Arguments: `{"command":"sleep 30"}`}}}},
		llm.ToolResultMessage("call-hydrate", "bash_run", "done"),
	}
	view, err = manager.GetHydrateView(r.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Transcript) != 2 || view.Transcript[0]["kind"] != "tool_call" || view.Transcript[1]["kind"] != "tool_result" {
		t.Fatalf("committed call/result ordering or deduplication = %#v", view.Transcript)
	}
}

func assertSingleProjectedCall(t *testing.T, view *HydrateView, id, status string) {
	t.Helper()
	if len(view.Transcript) != 1 {
		t.Fatalf("transcript entries = %#v", view.Transcript)
	}
	entry := view.Transcript[0]
	data, _ := entry["data"].(map[string]any)
	if entry["kind"] != "tool_call" || entry["blockId"] != id || data["tool_call_id"] != id {
		t.Fatalf("projected tool call = %#v", entry)
	}
	if len(view.TurnState.ToolExecutions) != 1 || string(view.TurnState.ToolExecutions[0].Status) != status {
		t.Fatalf("execution status = %#v, want %s", view.TurnState.ToolExecutions, status)
	}
}

type hydrateLongToolClient struct {
	calls atomic.Int32
	call  llm.ToolCall
}

func (c *hydrateLongToolClient) StreamChat(context.Context, llm.ChatRequest, llm.StreamHandler) (llm.ChatResult, error) {
	if c.calls.Add(1) == 1 {
		return llm.ChatResult{ToolCalls: []llm.ToolCall{c.call}, FinishReason: "tool_calls"}, nil
	}
	return llm.ChatResult{Content: "finished", FinishReason: "stop"}, nil
}
func (*hydrateLongToolClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "summary", nil
}
func (*hydrateLongToolClient) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func TestHydrateShowsRealLongRunningToolAndDeduplicatesAfterCompletion(t *testing.T) {
	workspace := t.TempDir()
	reg, err := tools.NewRegistry(workspace, 15)
	if err != nil {
		t.Fatal(err)
	}
	command := "sleep 3; printf done"
	if goruntime.GOOS == "windows" {
		command = "Start-Sleep -Seconds 3; Write-Output done"
	}
	call := llm.ToolCall{ID: "call-real-running", Type: "function", Function: llm.ToolCallFunction{
		Name: "bash_run", Arguments: fmt.Sprintf(`{"command":%q,"timeout_seconds":10}`, command),
	}}
	client := &hydrateLongToolClient{call: call}
	allow := policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"bash_run": policy.ModeNever}})
	mgr := NewManager("agent-1", stream.NewHub(64, logx.Discard()), client, reg, allow, nil,
		TurnOptions{SkillsEnabled: false, WorkspaceRoot: workspace}, logx.Discard())
	defer mgr.Stop()
	sess, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.EnqueueMessage(context.Background(), sess.ID, "message", "run the slow command", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	view := waitHydrateExecutionStatus(t, mgr, sess.ID, call.ID, turn.ToolExecutionStatusRunning, 8*time.Second)
	if len(view.Transcript) != 1 {
		t.Fatalf("running transcript = %#v", view.Transcript)
	}
	entry := view.Transcript[0]
	data, _ := entry["data"].(map[string]any)
	args, _ := data["arguments"].(map[string]any)
	if entry["kind"] != "tool_call" || entry["blockId"] != call.ID || data["tool_call_id"] != call.ID || args["command"] != command {
		t.Fatalf("running call/arguments projection = %#v", entry)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		view, err = mgr.GetHydrateView(sess.ID)
		if err != nil {
			t.Fatal(err)
		}
		if view.TurnState.Terminal {
			calls, results := 0, 0
			for _, item := range view.Transcript {
				switch item["kind"] {
				case "tool_call":
					calls++
				case "tool_result":
					results++
				}
			}
			if calls != 1 || results != 1 {
				t.Fatalf("completed transcript duplicates/misses call or result: %#v", view.Transcript)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("turn did not complete after the real tool returned")
}

func TestHydrateRetainsCancelledToolCallWithoutRunningStatus(t *testing.T) {
	workspace := t.TempDir()
	reg, err := tools.NewRegistry(workspace, 15)
	if err != nil {
		t.Fatal(err)
	}
	command := "sleep 30"
	if goruntime.GOOS == "windows" {
		command = "Start-Sleep -Seconds 30"
	}
	call := llm.ToolCall{ID: "call-cancelled", Type: "function", Function: llm.ToolCallFunction{
		Name: "bash_run", Arguments: fmt.Sprintf(`{"command":%q,"timeout_seconds":60}`, command),
	}}
	client := &hydrateLongToolClient{call: call}
	allow := policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"bash_run": policy.ModeNever}})
	mgr := NewManager("agent-1", stream.NewHub(64, logx.Discard()), client, reg, allow, nil,
		TurnOptions{SkillsEnabled: false, WorkspaceRoot: workspace}, logx.Discard())
	defer mgr.Stop()
	sess, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.EnqueueMessage(context.Background(), sess.ID, "message", "run then cancel", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	waitHydrateExecutionStatus(t, mgr, sess.ID, call.ID, turn.ToolExecutionStatusRunning, 8*time.Second)
	if !mgr.CancelTurn(sess.ID) {
		t.Fatal("expected active tool turn cancellation")
	}
	view, err := mgr.GetHydrateView(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	callEntries := 0
	for _, entry := range view.Transcript {
		data, _ := entry["data"].(map[string]any)
		if entry["kind"] == "tool_call" && entry["blockId"] == call.ID && data["tool_call_id"] == call.ID {
			callEntries++
		}
	}
	if !view.TurnState.Terminal || callEntries != 1 {
		t.Fatalf("cancelled call projection = terminal=%v transcript=%#v", view.TurnState.Terminal, view.Transcript)
	}
	status := turn.ToolExecutionStatusUnknown
	for _, execution := range view.TurnState.ToolExecutions {
		if execution.ToolCallID == call.ID {
			status = execution.Status
			break
		}
	}
	if status != turn.ToolExecutionStatusCancelled {
		t.Fatalf("cancelled call status = %s, want cancelled: %#v", status, view.TurnState.ToolExecutions)
	}
}

func waitHydrateExecutionStatus(t *testing.T, mgr *Manager, sessionID, callID string, status turn.ToolExecutionStatus, timeout time.Duration) *HydrateView {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		view, err := mgr.GetHydrateView(sessionID)
		if err != nil {
			t.Fatal(err)
		}
		for _, execution := range view.TurnState.ToolExecutions {
			if execution.ToolCallID == callID && execution.Status == status {
				return view
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("tool %s did not reach status %s", callID, status)
	return nil
}

func TestHydratePreservesParallelCallOrderWhenOneFinishes(t *testing.T) {
	now := time.Now().UTC()
	r := newLifecycleTestRuntime()
	r.queue = queue.NewMessageQueue()
	c := r.turnCoordinator
	for _, command := range []turn.TurnCommand{
		{Type: turn.CommandStartTurn, SessionID: r.session.ID, TurnID: "turn-parallel", Generation: 1, Source: turn.TurnSourceHuman, At: now},
		{Type: turn.CommandStartStep, SessionID: r.session.ID, TurnID: "turn-parallel", StepID: "step-parallel", Generation: 1, At: now},
		{Type: turn.CommandAssistantReceived, SessionID: r.session.ID, TurnID: "turn-parallel", StepID: "step-parallel", Generation: 1, HasTools: true, At: now},
		{Type: turn.CommandToolCallRecorded, SessionID: r.session.ID, TurnID: "turn-parallel", StepID: "step-parallel", Generation: 1, ToolCallID: "parallel-a", ToolName: "read_file", Arguments: []byte(`{"path":"first"}`), At: now},
		{Type: turn.CommandToolCallRecorded, SessionID: r.session.ID, TurnID: "turn-parallel", StepID: "step-parallel", Generation: 1, ToolCallID: "parallel-b", ToolName: "read_file", Arguments: []byte(`{"path":"second"}`), At: now},
		{Type: turn.CommandToolExecutionStarted, SessionID: r.session.ID, TurnID: "turn-parallel", StepID: "step-parallel", Generation: 1, ToolExecutionID: "parallel-a-execution", At: now},
		{Type: turn.CommandToolExecutionStarted, SessionID: r.session.ID, TurnID: "turn-parallel", StepID: "step-parallel", Generation: 1, ToolExecutionID: "parallel-b-execution", At: now},
		{Type: turn.CommandToolExecutionCompleted, SessionID: r.session.ID, TurnID: "turn-parallel", StepID: "step-parallel", Generation: 1, ToolExecutionID: "parallel-a-execution", ExecutionStatus: turn.ToolExecutionStatusSucceeded, At: now},
	} {
		if _, err := c.Dispatch(command); err != nil {
			t.Fatalf("dispatch %s: %v", command.Type, err)
		}
	}
	manager := NewManager("agent-1", nil, nil, nil, nil, nil, TurnOptions{}, logx.Discard())
	manager.sessions[r.session.ID] = r
	view, err := manager.GetHydrateView(r.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Transcript) != 2 || view.Transcript[0]["blockId"] != "parallel-a" || view.Transcript[1]["blockId"] != "parallel-b" {
		t.Fatalf("parallel call order = %#v", view.Transcript)
	}
	statusByCall := map[string]turn.ToolExecutionStatus{}
	for _, execution := range view.TurnState.ToolExecutions {
		statusByCall[execution.ToolCallID] = execution.Status
	}
	if statusByCall["parallel-a"] != turn.ToolExecutionStatusSucceeded || statusByCall["parallel-b"] != turn.ToolExecutionStatusRunning {
		t.Fatalf("parallel statuses = %v", statusByCall)
	}
}

func TestHydrateDoesNotCarryTerminalBatchIntoNewTurnBeforeStep(t *testing.T) {
	now := time.Now().UTC()
	r := newLifecycleTestRuntime()
	r.queue = queue.NewMessageQueue()
	c := r.turnCoordinator
	for _, command := range []turn.TurnCommand{
		{Type: turn.CommandStartTurn, SessionID: r.session.ID, TurnID: "turn-old", Generation: 1, Source: turn.TurnSourceHuman, At: now},
		{Type: turn.CommandStartStep, SessionID: r.session.ID, TurnID: "turn-old", StepID: "step-old", Generation: 1, At: now},
		{Type: turn.CommandAssistantReceived, SessionID: r.session.ID, TurnID: "turn-old", StepID: "step-old", Generation: 1, HasTools: true, At: now},
		{Type: turn.CommandToolCallRecorded, SessionID: r.session.ID, TurnID: "turn-old", StepID: "step-old", Generation: 1, ToolCallID: "old-call", ToolName: "bash_run", Arguments: []byte(`{"command":"sleep 30"}`), At: now},
		{Type: turn.CommandCancelTurn, SessionID: r.session.ID, TurnID: "turn-old", Generation: 1, At: now, Reason: "cancelled_by_user"},
		{Type: turn.CommandStartTurn, SessionID: r.session.ID, TurnID: "turn-new", Generation: 2, Source: turn.TurnSourceHuman, At: now},
	} {
		if _, err := c.Dispatch(command); err != nil {
			t.Fatalf("dispatch %s: %v", command.Type, err)
		}
	}
	manager := NewManager("agent-1", nil, nil, nil, nil, nil, TurnOptions{}, logx.Discard())
	manager.sessions[r.session.ID] = r
	view, err := manager.GetHydrateView(r.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.TurnState.TurnID != "turn-new" || len(view.Transcript) != 0 {
		t.Fatalf("new turn inherited previous batch before step: turn=%s transcript=%#v", view.TurnState.TurnID, view.Transcript)
	}
}
