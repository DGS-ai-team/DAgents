package session

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func TestHydrateProjectsActiveToolCallUntilTerminal(t *testing.T) {
	now := time.Now().UTC()
	st, err := store.Open(filepath.Join(t.TempDir(), "active-tool-hydrate.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
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
	manager := NewManager("agent-1", nil, nil, nil, nil, st, TurnOptions{}, logx.Discard())
	defer manager.Stop()
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

	// Once the assistant/tool messages are persisted, hydrate uses their durable
	// identity and suppresses the active projection without changing the ID.
	assistant := llm.Message{Role: "assistant", RecordID: "step-hydrate/assistant", ToolCalls: []llm.ToolCall{{ID: "call-hydrate", Type: "function", Function: llm.ToolCallFunction{Name: "bash_run", Arguments: `{"command":"sleep 30"}`}}}}
	result := llm.ToolResultMessage("call-hydrate", "bash_run", "done")
	result.RecordID = llm.ToolResultRecordID("call-hydrate")
	r.messages = []llm.Message{assistant, result}
	if err := st.SaveConversationSnapshot(context.Background(), store.Record{AgentID: r.session.ID, NodeID: r.session.AgentID, Messages: r.messages, RuntimeState: store.RuntimeState{HistoryRevision: 1}}); err != nil {
		t.Fatal(err)
	}
	view, err = manager.GetHydrateView(r.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Transcript) != 2 || view.Transcript[0]["kind"] != "tool_call" || view.Transcript[1]["kind"] != "tool_result" {
		t.Fatalf("committed call/result ordering or deduplication = %#v", view.Transcript)
	}
	wantCallItemID := ToolCallTranscriptItemID("step-hydrate/assistant", "call-hydrate")
	if view.Transcript[0]["item_id"] != wantCallItemID || view.Transcript[0]["id"] != wantCallItemID {
		t.Fatalf("persisted tool call lost active projection identity continuity: %#v", view.Transcript[0])
	}
	if view.Transcript[0]["item_version"] != 1 {
		t.Fatalf("persisted tool call version = %#v, want row revision 1", view.Transcript[0]["item_version"])
	}
}

func assertSingleProjectedCall(t *testing.T, view *HydrateView, id, status string) {
	t.Helper()
	if len(view.Transcript) != 1 {
		t.Fatalf("transcript entries = %#v", view.Transcript)
	}
	entry := view.Transcript[0]
	data, _ := entry["data"].(map[string]any)
	wantItemID := ToolCallTranscriptItemID("step-hydrate/assistant", id)
	if entry["kind"] != "tool_call" || entry["blockId"] != id || data["tool_call_id"] != id || entry["item_id"] != wantItemID || entry["id"] != wantItemID {
		t.Fatalf("projected tool call = %#v", entry)
	}
	if _, hasVersion := entry["item_version"]; hasVersion {
		t.Fatalf("live active tool projection claimed an uncommitted item version: %#v", entry)
	}
	if len(view.TurnState.ToolExecutions) != 1 || string(view.TurnState.ToolExecutions[0].Status) != status {
		t.Fatalf("execution status = %#v, want %s", view.TurnState.ToolExecutions, status)
	}
}

func TestMergeActiveToolCallsUsesStableIDAndDeduplicates(t *testing.T) {
	calls := []turn.ToolCallProjection{{ID: "call-duplicate", ToolName: "read_file", Arguments: []byte(`{"path":"x"}`), StepID: "step-1", AssistantMessageID: "step-1/assistant", BatchIndex: 0}}
	entries := mergeActiveToolCallsAtAnchor(nil, calls)
	entries = mergeActiveToolCallsAtAnchor(entries, calls)
	entries = mergeActiveToolCallsAtAnchor(entries, append(calls, calls[0]))
	if len(entries) != 1 {
		t.Fatalf("duplicate active projection entries=%#v, want one", entries)
	}
	entry := entries[0]
	wantItemID := ToolCallTranscriptItemID("step-1/assistant", "call-duplicate")
	if entry["item_id"] != wantItemID || entry["id"] != wantItemID {
		t.Fatalf("active projection identity = %#v", entry)
	}
	if _, hasVersion := entry["item_version"]; hasVersion {
		t.Fatalf("active projection fabricated item version: %#v", entry)
	}
}

func TestMergeActiveToolCallUsesAssistantOrderAndDoesNotDisappearWhenResultLeads(t *testing.T) {
	calls := []turn.ToolCallProjection{{
		ID: "call-anchored", ToolName: "read_file", Arguments: []byte(`{"path":"x"}`),
		StepID: "step-anchored", AssistantMessageID: "step-anchored/assistant", BatchIndex: 0,
	}}
	transcript := []TranscriptEntry{
		{"kind": "user", "item_id": "input-1", "conversation_ordinal": int64(4)},
		{
			"kind": "assistant", "item_id": "step-anchored/assistant/content", "detail_item_id": "step-anchored/assistant",
			"conversation_ordinal": int64(5), "conversation_entry_index": 0,
			"round_id": "round-1", "round_ordinal": int64(2), "round_revision_id": "revision-1",
		},
		{"kind": "tool_result", "blockId": "call-anchored", "data": map[string]any{"tool_call_id": "call-anchored"}, "conversation_ordinal": int64(6)},
	}
	merged := mergeActiveToolCallsAtAnchor(transcript, calls)
	if len(merged) != 4 || merged[0]["kind"] != "user" || merged[1]["kind"] != "assistant" || merged[2]["kind"] != "tool_call" || merged[3]["kind"] != "tool_result" {
		t.Fatalf("active tool call was not placed after its assistant parent: %#v", merged)
	}
	call := merged[2]
	wantItemID := ToolCallTranscriptItemID("step-anchored/assistant", "call-anchored")
	if call["item_id"] != wantItemID || call["parent_item_id"] != "step-anchored/assistant" || call["step_id"] != "step-anchored" || call["tool_batch_index"] != 0 {
		t.Fatalf("active call identity/anchor = %#v", call)
	}
	if call["conversation_ordinal"] != int64(5) || call["conversation_entry_index"] != 1 || call["round_id"] != "round-1" || call["round_revision_id"] != "revision-1" {
		t.Fatalf("active call order metadata = %#v", call)
	}

	resultOnly := []TranscriptEntry{
		{"kind": "user", "item_id": "input-2"},
		{"kind": "tool_result", "blockId": "call-anchored", "data": map[string]any{"tool_call_id": "call-anchored"}},
	}
	merged = mergeActiveToolCallsAtAnchor(resultOnly, calls)
	if len(merged) != 3 || merged[1]["kind"] != "tool_call" || merged[2]["kind"] != "tool_result" {
		t.Fatalf("active call was lost or placed after its result: %#v", merged)
	}

	// Even if another persisted call appears after an early result, the missing
	// earlier call must not be inserted after that result just to match the
	// later call's batch index.
	calls = []turn.ToolCallProjection{
		{ID: "call-first", ToolName: "read_file", Arguments: []byte(`{"path":"first"}`), BatchIndex: 0},
		{ID: "call-last", ToolName: "read_file", Arguments: []byte(`{"path":"last"}`), BatchIndex: 2},
	}
	transcript = []TranscriptEntry{
		{"kind": "tool_result", "blockId": "call-first", "data": map[string]any{"tool_call_id": "call-first"}},
		{"kind": "tool_call", "blockId": "call-last", "data": map[string]any{"tool_call_id": "call-last"}},
	}
	merged = mergeActiveToolCallsAtAnchor(transcript, calls)
	if len(merged) != 3 || merged[0]["kind"] != "tool_call" || merged[0]["blockId"] != "call-first" {
		t.Fatalf("call was inserted after a leading tool result: %#v", merged)
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

func TestHydrateClearBoundaryHidesCancelledCallButRetainsRecoveryFact(t *testing.T) {
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
	dbPath := filepath.Join(t.TempDir(), "cancelled-hydrate.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	mgr := NewManager("agent-1", stream.NewHub(64, logx.Discard()), client, reg, allow, st,
		TurnOptions{SkillsEnabled: false, WorkspaceRoot: workspace}, logx.Discard())
	defer func() {
		mgr.Stop()
		_ = st.Close()
	}()
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

	if _, err := mgr.ClearContext(sess.ID); err != nil {
		t.Fatal(err)
	}
	cleared, err := mgr.GetHydrateView(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cleared.Transcript) != 0 {
		t.Fatalf("cleared transcript resurrected lifecycle tool call: %#v", cleared.Transcript)
	}
	if len(cleared.TurnState.ToolExecutions) != 1 || cleared.TurnState.ToolExecutions[0].Status != turn.ToolExecutionStatusCancelled {
		t.Fatalf("clear discarded terminal recovery facts: %#v", cleared.TurnState.ToolExecutions)
	}
	mgr.Stop()
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	nextCommand := "sleep 3; printf next"
	if goruntime.GOOS == "windows" {
		nextCommand = "Start-Sleep -Seconds 3; Write-Output next"
	}
	nextCall := llm.ToolCall{ID: "call-after-clear", Type: "function", Function: llm.ToolCallFunction{
		Name: "bash_run", Arguments: fmt.Sprintf(`{"command":%q,"timeout_seconds":10}`, nextCommand),
	}}
	restartClient := &hydrateLongToolClient{call: nextCall}
	restarted := NewManager("agent-1", stream.NewHub(64, logx.Discard()), restartClient, reg, allow, reopened,
		TurnOptions{SkillsEnabled: false, WorkspaceRoot: workspace}, logx.Discard())
	defer restarted.Stop()
	view, err = restarted.GetHydrateView(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Transcript) != 0 {
		t.Fatalf("restart resurrected pre-clear call projection: %#v", view.Transcript)
	}
	if len(view.TurnState.ToolExecutions) != 1 || view.TurnState.ToolExecutions[0].Status != turn.ToolExecutionStatusCancelled {
		t.Fatalf("restart lost lifecycle recovery facts: %#v", view.TurnState.ToolExecutions)
	}
	if _, _, err := restarted.Create(sess.ID); err != nil {
		t.Fatalf("restore runtime after clear: %v", err)
	}
	if _, err := restarted.EnqueueMessage(context.Background(), sess.ID, "message", "run a new tool", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	newView := waitHydrateExecutionStatus(t, restarted, sess.ID, nextCall.ID, turn.ToolExecutionStatusRunning, 8*time.Second)
	if len(newView.Transcript) != 2 || newView.Transcript[0]["kind"] != "user" || newView.Transcript[0]["text"] != "run a new tool" || newView.Transcript[1]["kind"] != "tool_call" || newView.Transcript[1]["blockId"] != nextCall.ID {
		t.Fatalf("new turn after clear did not preserve the user message before its active tool call: %#v", newView.Transcript)
	}
}

func TestClearMessagesPersistFailureDoesNotPublishPartialSnapshot(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clear-failure.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	r := newLifecycleTestRuntime()
	r.store = st
	r.logger = logx.Discard()
	r.messages = []llm.Message{{Role: "user", Content: "before clear"}}
	r.messages[0].RecordID, err = llm.NewOpaqueRecordID()
	if err != nil {
		r.mu.Unlock()
		t.Fatal(err)
	}
	r.noteRecordUpsertLocked(r.messages[0])
	r.noteCheckpointDirtyLocked()
	r.historyRevision = 7
	if err := r.persist(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	triggerDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = triggerDB.Exec(`CREATE TRIGGER reject_runtime_snapshot BEFORE UPDATE ON agent_runtimes
WHEN NEW.runtime_state_json != '{}'
BEGIN SELECT RAISE(ABORT, 'injected snapshot failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	if err := triggerDB.Close(); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager("agent-1", nil, nil, nil, nil, st, TurnOptions{}, r.logger)
	mgr.sessions[r.session.ID] = r
	if _, err := mgr.ClearContext(r.session.ID); err == nil {
		t.Fatal("ClearContext reported success after its snapshot write failed")
	} else if !strings.Contains(err.Error(), "injected snapshot failure") {
		t.Fatalf("ClearContext persistence error = %v", err)
	}

	rec, err := st.Load(context.Background(), r.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil || len(rec.Messages) != 1 || rec.Messages[0].Content != "before clear" {
		t.Fatalf("failed clear published partial transcript: %#v", rec)
	}
	if rec.RuntimeState.HistoryRevision != 7 || rec.RuntimeState.ToolCallProjectionHiddenThroughGeneration != 0 {
		t.Fatalf("failed clear published partial runtime state: %#v", rec.RuntimeState)
	}
}

func TestSuccessfulClearResetsOldHistoryIdentityFailure(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "clear-identity-failure.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	r := newLifecycleTestRuntime()
	r.store = st
	r.logger = logx.Discard()
	if err := st.Save(ctx, store.Record{AgentID: r.session.ID, NodeID: r.session.AgentID}); err != nil {
		t.Fatal(err)
	}
	r.deltaMu.Lock()
	r.historyMutationErr = fmt.Errorf("old transcript identity allocation failed")
	r.deltaMu.Unlock()
	manager := NewManager("agent-1", nil, nil, nil, nil, st, TurnOptions{}, r.logger)
	manager.sessions[r.session.ID] = r
	if _, err := manager.ClearContext(r.session.ID); err != nil {
		t.Fatalf("clear failed: %v", err)
	}

	message := llm.Message{Role: "assistant", Content: "saved after clear", RecordID: "step-after-clear/assistant/content"}
	r.mu.Lock()
	r.messages = []llm.Message{message}
	r.historyRevision++
	r.noteRecordUpsertLocked(message)
	r.noteCheckpointDirtyLocked()
	r.mu.Unlock()
	if err := r.persist(ctx); err != nil {
		t.Fatalf("persist after clear: %v", err)
	}
	records, err := st.LoadConversationRecords(ctx, r.session.ID)
	if err != nil || len(records) != 1 || records[0].Content != message.Content {
		t.Fatalf("records after recovery clear = %+v err=%v", records, err)
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
