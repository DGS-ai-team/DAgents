package session

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func newLifecycleTestRuntime() *runtime {
	return &runtime{
		session:         Session{ID: "session-1", AgentID: "agent-1"},
		turnCoordinator: turn.NewTurnCoordinator("session-1", "agent-1"),
	}
}

func setTestPendingHITL(t *testing.T, r *runtime, pending *turn.PendingHITL) {
	t.Helper()
	r.restoreLegacyPending(pending)
	if got := r.pendingSnapshot(); got == nil {
		t.Fatalf("pending HITL projection was not installed: %#v", pending)
	}
}

func TestRuntimeLifecycleBridgesToolContinuation(t *testing.T) {
	r := newLifecycleTestRuntime()
	r.lifecycleBeginHumanTurn()

	first := r.turnCoordinator.Snapshot()
	if first.TurnStatus != turn.TurnStatusRunning || first.StepStatus != turn.StepStatusRequesting {
		t.Fatalf("initial lifecycle projection = %#v", first)
	}

	r.lifecycleAfterModelStep(turn.StepOutcome{}, []llm.Message{{
		Role: "assistant",
		ToolCalls: []llm.ToolCall{{
			ID: "call-1",
		}},
	}, {
		Role:       "tool",
		ToolCallID: "call-1",
		Content:    "ok",
	}}, 0)
	completedStep := r.turnCoordinator.Snapshot()
	if completedStep.StepStatus != turn.StepStatusExecutingTools || completedStep.TurnStatus != turn.TurnStatusRunning {
		t.Fatalf("tool execution projection = %#v", completedStep)
	}

	r.lifecycleBeginContinuationStep(turn.TurnSourceHuman)
	second := r.turnCoordinator.Snapshot()
	if second.StepIndex != 2 || second.StepStatus != turn.StepStatusRequesting {
		t.Fatalf("continuation step projection = %#v", second)
	}

	r.lifecycleAfterModelStep(turn.StepOutcome{}, []llm.Message{{Role: "assistant"}}, 0)
	finished := r.turnCoordinator.Snapshot()
	if finished.TurnStatus != turn.TurnStatusCompleted || finished.HasActiveTurn {
		t.Fatalf("finished lifecycle projection = %#v", finished)
	}
}

func TestRuntimeLifecyclePublishesTurnStateProjection(t *testing.T) {
	hub := stream.NewHub(32, logx.Discard())
	r := newLifecycleTestRuntime()
	r.hub = hub
	r.agentID = "agent-1"
	r.queue = queue.NewMessageQueue()
	events := hub.SubscribeAgent(0, "session-1")
	defer hub.Unsubscribe(events)

	if err := r.lifecycleBeginHumanTurn(); err != nil {
		t.Fatal(err)
	}
	if err := r.lifecycleAfterModelStep(turn.StepOutcome{}, []llm.Message{{Role: "assistant", Content: "done"}}, 0); err != nil {
		t.Fatal(err)
	}

	phases := make([]string, 0, 5)
	var terminalHistoryRevision any
	for len(phases) < cap(phases) {
		select {
		case event := <-events:
			if event.Type != "turn_state" {
				continue
			}
			phase, _ := event.Data["phase"].(string)
			phases = append(phases, phase)
			if phase == "completed" {
				terminalHistoryRevision = event.Data["history_revision"]
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for turn_state events; phases=%v", phases)
		}
	}
	if phases[0] != "queued" || phases[1] != "model_generating" {
		t.Fatalf("initial turn_state phases = %v", phases)
	}
	if phases[len(phases)-1] != "completed" {
		t.Fatalf("terminal turn_state phase = %q, all=%v", phases[len(phases)-1], phases)
	}
	if terminalHistoryRevision != uint64(1) {
		t.Fatalf("terminal history revision event = %#v", terminalHistoryRevision)
	}
	if r.historyRevision != 1 || len(r.messages) != 1 || r.messages[0].Content != "done" {
		t.Fatalf("terminal history projection = revision=%d messages=%#v", r.historyRevision, r.messages)
	}
	manager := NewManager("agent-1", hub, nil, nil, nil, nil, TurnOptions{}, logx.Discard())
	manager.sessions[r.session.ID] = r
	view, err := manager.GetHydrateView(r.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !view.TurnState.Terminal || view.HistoryRevision != 1 || view.TurnState.HistoryRevision != 1 {
		t.Fatalf("terminal hydrate state = %#v", view)
	}
	if len(view.Transcript) != 1 || view.Transcript[0]["kind"] != "assistant" || view.Transcript[0]["text"] != "done" {
		t.Fatalf("terminal hydrate transcript = %#v", view.Transcript)
	}
}

func TestRuntimeLifecycleBridgesHITLResume(t *testing.T) {
	r := newLifecycleTestRuntime()
	r.lifecycleBeginHumanTurn()
	r.lifecycleAfterModelStep(turn.StepOutcome{
		Pending: &turn.PendingHITL{Items: []turn.PendingHITLItem{{
			ToolCall: llm.ToolCall{ID: "call-approval"},
		}}},
	}, []llm.Message{{
		Role:      "assistant",
		ToolCalls: []llm.ToolCall{{ID: "call-approval"}},
	}}, 0)
	waiting := r.turnCoordinator.Snapshot()
	if waiting.TurnStatus != turn.TurnStatusWaiting || waiting.StepStatus != turn.StepStatusWaitingInteraction {
		t.Fatalf("waiting lifecycle projection = %#v", waiting)
	}

	r.lifecyclePrepareResume(nil)
	resumed := r.turnCoordinator.Snapshot()
	if resumed.StepStatus != turn.StepStatusExecutingTools || resumed.TurnStatus != turn.TurnStatusRunning {
		t.Fatalf("resumed lifecycle projection = %#v", resumed)
	}
	r.lifecycleAfterResume(turn.StepOutcome{}, []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call-approval"}}},
		{Role: "tool", ToolCallID: "call-approval", Content: "approved"},
	})
	settled := r.turnCoordinator.Snapshot()
	if settled.StepStatus != turn.StepStatusCompleted || settled.TurnStatus != turn.TurnStatusCompleted {
		t.Fatalf("settled lifecycle projection = %#v", settled)
	}
}

func TestRuntimeLifecycleAfterResumePersistsRemainingHITL(t *testing.T) {
	r := newLifecycleTestRuntime()
	if err := r.lifecycleBeginHumanTurn(); err != nil {
		t.Fatal(err)
	}
	initial := &turn.PendingHITL{Items: []turn.PendingHITLItem{
		{ToolCall: llm.ToolCall{ID: "call-question", Function: llm.ToolCallFunction{Name: "ask_user_information"}}},
		{ToolCall: llm.ToolCall{ID: "call-bash", Function: llm.ToolCallFunction{Name: "bash_run"}}},
	}}
	history := []llm.Message{{Role: "assistant", ToolCalls: initial.AllToolCalls()}}
	if err := r.lifecycleAfterModelStep(turn.StepOutcome{Pending: initial}, history, 0); err != nil {
		t.Fatal(err)
	}
	if err := r.lifecyclePrepareResume(map[string]any{"type": "user_information", "tool_call_id": "call-question"}); err != nil {
		t.Fatal(err)
	}

	remaining := &turn.PendingHITL{Items: []turn.PendingHITLItem{initial.Items[1]}}
	history = append(history, llm.ToolResultMessage("call-question", "ask_user_information", "[USER_INFORMATION] answer=\"ok\""))
	if err := r.lifecycleAfterResume(turn.StepOutcome{Pending: remaining}, history); err != nil {
		t.Fatal(err)
	}

	snapshot := r.turnCoordinator.Snapshot()
	if snapshot.StepStatus != turn.StepStatusWaitingInteraction {
		t.Fatalf("remaining HITL lifecycle status = %#v", snapshot)
	}
	var projected turn.PendingHITL
	if err := json.Unmarshal(snapshot.InteractionPayload, &projected); err != nil {
		t.Fatalf("decode remaining HITL payload: %v", err)
	}
	if len(projected.Items) != 1 || projected.Items[0].ToolCall.ID != "call-bash" {
		t.Fatalf("remaining HITL payload = %#v", projected)
	}
	if pending := r.pendingSnapshot(); pending == nil || len(pending.Items) != 1 || pending.Items[0].ToolCall.ID != "call-bash" {
		t.Fatalf("pending projection = %#v", pending)
	}
}

func TestPendingFromLifecycleSnapshotSkipsInternalSideEffectCallback(t *testing.T) {
	state := turn.CoordinatorSnapshot{StepStatus: turn.StepStatusWaitingInteraction}
	history := []llm.Message{{
		Role: "assistant",
		ToolCalls: []llm.ToolCall{{
			ID:       "async-job-1",
			Function: llm.ToolCallFunction{Name: "tool_callback"},
		}},
	}}

	if pending := pendingFromLifecycleSnapshot(state, history); pending != nil {
		t.Fatalf("internal side-effect callback must not become pending HITL: %#v", pending)
	}
}

func TestLifecycleRecordToolFactsSkipsInternalSideEffectCallback(t *testing.T) {
	r := newLifecycleTestRuntime()
	if err := r.lifecycleBeginHumanTurn(); err != nil {
		t.Fatal(err)
	}
	call := llm.ToolCall{
		ID:       "async-job-1",
		Function: llm.ToolCallFunction{Name: "bash_run"},
	}
	history := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{call}},
		{Role: "tool", ToolCallID: call.ID, Content: "callback result"},
	}
	if err := r.lifecycleRecordToolFacts(history, 0, []llm.ToolCall{call}); err != nil {
		t.Fatal(err)
	}
	if r.turnCoordinator.HasToolCall(call.ID) {
		t.Fatal("an unknown history call must not be recorded as executable tool")
	}
}

func TestPendingSnapshotDoesNotReconstructFromHistory(t *testing.T) {
	r := newLifecycleTestRuntime()
	if err := r.lifecycleBeginHumanTurn(); err != nil {
		t.Fatal(err)
	}
	state := r.turnCoordinator.Snapshot()
	if _, err := r.lifecycleDispatchErr(turn.TurnCommand{
		Type:       turn.CommandAssistantReceived,
		SessionID:  r.session.ID,
		TurnID:     state.TurnID,
		StepID:     state.StepID,
		Generation: state.Generation,
		HasTools:   true,
		At:         time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	state = r.turnCoordinator.Snapshot()
	if _, err := r.lifecycleDispatchErr(turn.TurnCommand{
		Type:       turn.CommandInteractionRequested,
		SessionID:  r.session.ID,
		TurnID:     state.TurnID,
		StepID:     state.StepID,
		Generation: state.Generation,
		At:         time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	r.messages = []llm.Message{{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "history-only"}}}}
	if pending := r.pendingSnapshot(); pending != nil {
		t.Fatalf("history-only tool call must not create active HITL: %#v", pending)
	}
}

func TestRuntimeLifecycleCancel(t *testing.T) {
	r := newLifecycleTestRuntime()
	r.lifecycleBeginHumanTurn()
	r.lifecycleCancel()

	snapshot := r.turnCoordinator.Snapshot()
	if snapshot.TurnStatus != turn.TurnStatusCancelled || snapshot.StepStatus != turn.StepStatusCancelled {
		t.Fatalf("cancelled lifecycle projection = %#v", snapshot)
	}
}

func TestColdViewsPreferDurableLifecycleProjection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cold-projection.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	r := newLifecycleTestRuntime()
	r.store = st
	r.logger = logx.Discard()
	r.lifecycleBeginHumanTurn()
	pending := &turn.PendingHITL{Items: []turn.PendingHITLItem{{
		ToolCall: llm.ToolCall{ID: "call-cold", Function: llm.ToolCallFunction{Name: "require_approval", Arguments: `{"reason":"test"}`}},
	}}}
	history := []llm.Message{{Role: "assistant", ToolCalls: pending.AllToolCalls()}}
	r.lifecycleAfterModelStep(turn.StepOutcome{Pending: pending}, history, 0)

	// Deliberately make the deprecated mirror disagree with the event projection.
	if err := st.Save(context.Background(), store.Record{
		AgentID:  "session-1",
		NodeID:   "agent-1",
		Messages: history,
		RuntimeState: store.RuntimeState{
			ToolLoopCount: 99,
			NotifySeq:     8,
			AckSeq:        3,
		},
	}); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager("agent-1", stream.NewHub(8, logx.Discard()), &llm.MockClient{}, nil, nil, st, TurnOptions{}, logx.Discard())
	defer mgr.Stop()

	contextView, err := mgr.GetContextView("session-1")
	if err != nil {
		t.Fatal(err)
	}
	if !contextView.HasActiveTurn || contextView.StepStatus != turn.StepStatusWaitingInteraction || contextView.ToolLoopCount != 1 {
		t.Fatalf("context view did not use lifecycle projection: %#v", contextView)
	}

	hydrate, err := mgr.GetHydrateView("session-1")
	if err != nil {
		t.Fatal(err)
	}
	if !hydrate.HasActiveTurn || len(hydrate.PendingHITL) == 0 {
		t.Fatalf("hydrate view did not use lifecycle projection: %#v", hydrate)
	}

	notification := mgr.NotificationState("session-1")
	if !notification.HasPendingHITL || notification.PendingHITLItems != 1 || notification.NotifySeq != 8 || notification.AckSeq != 3 {
		t.Fatalf("notification state did not use lifecycle projection: %#v", notification)
	}
}

func TestRuntimeLifecycleCancelPropagatesPersistenceFailure(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "cancel-failure.db"))
	if err != nil {
		t.Fatal(err)
	}
	r := newLifecycleTestRuntime()
	r.store = st
	if err := r.lifecycleBeginHumanTurn(); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	if err := r.lifecycleCancel(); err == nil {
		t.Fatal("expected lifecycle cancellation persistence error")
	}
	snapshot := r.turnCoordinator.Snapshot()
	if snapshot.TurnStatus != turn.TurnStatusRunning || snapshot.StepStatus != turn.StepStatusRequesting {
		t.Fatalf("failed cancellation must roll back projection = %#v", snapshot)
	}
}

func TestRuntimeLifecycleContextCompactionPropagatesPersistenceFailure(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "compaction-failure.db"))
	if err != nil {
		t.Fatal(err)
	}
	r := newLifecycleTestRuntime()
	r.store = st
	if err := r.lifecycleBeginHumanTurn(); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	if err := r.lifecycleContextCompacted("test", "before", "after", 4, 2); err == nil {
		t.Fatal("expected context compaction persistence error")
	}
	snapshot := r.turnCoordinator.Snapshot()
	if snapshot.ContextEpoch != 0 || snapshot.StepStatus != turn.StepStatusRequesting {
		t.Fatalf("failed compaction must roll back projection = %#v", snapshot)
	}
}

func TestRuntimeLifecycleAfterModelStepPropagatesPersistenceFailure(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "model-step-failure.db"))
	if err != nil {
		t.Fatal(err)
	}
	r := newLifecycleTestRuntime()
	r.store = st
	if err := r.lifecycleBeginHumanTurn(); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	err = r.lifecycleAfterModelStep(turn.StepOutcome{}, []llm.Message{{Role: "assistant", Content: "done"}}, 0)
	if err == nil {
		t.Fatal("expected model step lifecycle persistence error")
	}
	snapshot := r.turnCoordinator.Snapshot()
	if snapshot.TurnStatus != turn.TurnStatusRunning || snapshot.StepStatus != turn.StepStatusRequesting {
		t.Fatalf("failed model step must roll back projection = %#v", snapshot)
	}
}

func TestRuntimeLifecycleStopsContinuationAtStepBudget(t *testing.T) {
	r := newLifecycleTestRuntime()
	r.turnBudget = turn.TurnBudget{MaxSteps: 1}
	r.lifecycleBeginHumanTurn()
	r.lifecycleAfterModelStep(turn.StepOutcome{}, []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call-budget"}}},
		{Role: "tool", ToolCallID: "call-budget", Content: "ok"},
	}, 0)
	if started, err := r.lifecycleBeginContinuationStep(turn.TurnSourceHuman); err != nil {
		t.Fatal(err)
	} else if started {
		t.Fatal("continuation should be rejected after MaxSteps")
	}
	snapshot := r.turnCoordinator.Snapshot()
	if snapshot.TurnStatus != turn.TurnStatusBudgetExhausted || snapshot.TurnEndReason == "" {
		t.Fatalf("budget projection = %#v", snapshot)
	}
}

func TestRuntimeLifecycleUsesReservedFinalSummaryStep(t *testing.T) {
	r := newLifecycleTestRuntime()
	r.turnBudget = turn.TurnBudget{MaxSteps: 1, ReserveFinalSummary: true}
	r.lifecycleBeginHumanTurn()
	r.lifecycleAfterModelStep(turn.StepOutcome{}, []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call-summary"}}},
		{Role: "tool", ToolCallID: "call-summary", Content: "tool result"},
	}, 0)
	if started, err := r.lifecycleBeginContinuationStep(turn.TurnSourceHuman); err != nil {
		t.Fatal(err)
	} else if !started {
		t.Fatal("reserved summary continuation should be accepted")
	}
	state := r.turnCoordinator.Snapshot()
	if !state.FinalSummary || state.StepIndex != 2 || state.Usage.SummarySteps != 1 {
		t.Fatalf("summary continuation projection = %+v", state)
	}
}

func TestRuntimeLifecycleDoesNotReusePreviousAssistant(t *testing.T) {
	r := newLifecycleTestRuntime()
	r.lifecycleBeginHumanTurn()

	oldHistory := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "old-call"}}},
		{Role: "user", Content: "new input"},
	}
	r.lifecycleAfterModelStep(turn.StepOutcome{}, oldHistory, len(oldHistory))

	snapshot := r.turnCoordinator.Snapshot()
	if snapshot.StepStatus != turn.StepStatusCompleted || snapshot.TurnStatus != turn.TurnStatusCompleted {
		t.Fatalf("lifecycle reused old assistant = %#v", snapshot)
	}
}

func TestRuntimeLifecyclePersistsTurnAndStepEvents(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	r := newLifecycleTestRuntime()
	r.store = st
	r.lifecycleBeginHumanTurn()

	events, err := st.ListTurnEvents(context.Background(), "session-1", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].EventType != turn.EventTurnStarted || events[1].EventType != turn.EventStepStarted {
		t.Fatalf("lifecycle events = %#v", events)
	}
	if events[0].SessionSeq != 1 || events[1].SessionSeq != 2 || events[1].TurnSeq != 2 {
		t.Fatalf("lifecycle sequences = %#v", events)
	}
	recovered := newLifecycleTestRuntime()
	recovered.store = st
	recovered.restoreLifecycleEvents()
	snapshot := recovered.turnCoordinator.Snapshot()
	if snapshot.TurnID == "" || snapshot.StepStatus != turn.StepStatusRequesting || snapshot.Generation == 0 {
		t.Fatalf("recovered lifecycle projection = %#v", snapshot)
	}
	if got := recovered.turnCoordinator.ExecutionContext(); !got.Valid() || got.TurnID != snapshot.TurnID || got.Generation != snapshot.Generation {
		t.Fatalf("recovered execution context=%#v projection=%#v", got, snapshot)
	}
}

func TestRuntimeLifecyclePersistsProviderCacheUsage(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "cache-usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	r := newLifecycleTestRuntime()
	r.store = st
	if err := r.lifecycleBeginHumanTurn(); err != nil {
		t.Fatal(err)
	}
	state := r.turnCoordinator.Snapshot()
	if _, err := r.lifecycleDispatchErr(turn.TurnCommand{
		Type:       turn.CommandModelRequestStarted,
		SessionID:  "session-1",
		TurnID:     state.TurnID,
		StepID:     state.StepID,
		Generation: state.Generation,
		At:         time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.lifecycleDispatchErr(turn.TurnCommand{
		Type:       turn.CommandModelUsageRecorded,
		SessionID:  "session-1",
		TurnID:     state.TurnID,
		StepID:     state.StepID,
		Generation: state.Generation,
		Usage: turn.StepUsage{
			InputTokens:                100,
			OutputTokens:               8,
			TotalTokens:                108,
			PromptCacheHitTokens:       80,
			PromptCacheMissTokens:      20,
			PromptCacheMetricsObserved: true,
		},
		At: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	events, err := st.ListTurnEvents(context.Background(), "session-1", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events {
		if event.EventType != turn.EventModelUsageRecorded {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		usage, ok := payload["usage"].(map[string]any)
		if !ok {
			t.Fatalf("usage payload = %#v", payload["usage"])
		}
		if usage["prompt_cache_hit_tokens"] != float64(80) || usage["prompt_cache_miss_tokens"] != float64(20) || usage["prompt_cache_metrics_observed"] != true {
			t.Fatalf("persisted cache usage = %#v", usage)
		}
		found = true
		break
	}
	if !found {
		t.Fatal("model usage event was not persisted")
	}
	recovered := newLifecycleTestRuntime()
	recovered.store = st
	recovered.restoreLifecycleEvents()
	recoveredUsage := recovered.turnCoordinator.Snapshot().Usage
	if recoveredUsage.PromptCacheHitTokens != 80 || recoveredUsage.PromptCacheMissTokens != 20 || !recoveredUsage.PromptCacheMetricsObserved {
		t.Fatalf("replayed cache usage = %+v", recoveredUsage)
	}
}

func TestRuntimeLifecycleRestoresCommandSequenceAfterTerminalTurn(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "terminal-sequence.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	first := newLifecycleTestRuntime()
	first.store = st
	first.lifecycleBeginHumanTurn()
	first.lifecycleAfterModelStep(turn.StepOutcome{}, []llm.Message{{Role: "assistant", Content: "done"}}, 0)
	initialEvents, err := st.ListTurnEvents(context.Background(), "session-1", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(initialEvents) == 0 {
		t.Fatal("expected terminal turn events")
	}

	second := newLifecycleTestRuntime()
	second.store = st
	second.restoreLifecycleEvents()
	second.lifecycleBeginHumanTurn()
	allEvents, err := st.ListTurnEvents(context.Background(), "session-1", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(allEvents) <= len(initialEvents) {
		t.Fatalf("new turn events were not appended: initial=%d all=%d", len(initialEvents), len(allEvents))
	}
	seenCommands := make(map[string]struct{})
	for _, event := range allEvents {
		if event.CommandID == "" {
			continue
		}
		if _, exists := seenCommands[event.CommandID]; exists {
			t.Fatalf("duplicate lifecycle command id after terminal restore: %s", event.CommandID)
		}
		seenCommands[event.CommandID] = struct{}{}
	}
}

func TestRuntimeLifecycleMarksUnknownAndContinuesOnlyAfterReconciliation(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "recovery.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	newRuntime := func() *runtime {
		hub := stream.NewHub(16, logx.Discard())
		return newRuntimeWithPublisher(
			"session-1", "agent-1", hub, hub,
			&llm.MockClient{}, nil, nil, st, logx.Discard(), nil, nil, nil, 0, nil,
			false, 0, 0, TurnOptions{FSRoot: t.TempDir(), SkillsEnabled: false}, nil,
		)
	}

	first := newRuntime()
	first.lifecycleBeginHumanTurn()
	state := first.turnCoordinator.Snapshot()
	first.lifecycleDispatch(turn.TurnCommand{
		Type: turn.CommandAssistantReceived, SessionID: "session-1", TurnID: state.TurnID,
		StepID: state.StepID, Generation: state.Generation, HasTools: true, At: time.Now().UTC(),
	})
	state = first.turnCoordinator.Snapshot()
	first.lifecycleDispatch(turn.TurnCommand{
		Type: turn.CommandToolCallRecorded, SessionID: "session-1", TurnID: state.TurnID,
		StepID: state.StepID, Generation: state.Generation, ToolCallID: "call-restart",
		ToolName: "bash", Arguments: []byte(`{"command":"touch marker"}`), At: time.Now().UTC(),
	})
	state = first.turnCoordinator.Snapshot()
	first.lifecycleDispatch(turn.TurnCommand{
		Type: turn.CommandToolExecutionStarted, SessionID: "session-1", TurnID: state.TurnID,
		StepID: state.StepID, Generation: state.Generation, ToolCallID: "call-restart",
		ToolExecutionID: "call-restart-execution", At: time.Now().UTC(),
	})

	second := newRuntime()
	second.restoreLifecycleEvents()
	recovered := second.turnCoordinator.Snapshot()
	if !recovered.RecoveryRequired || recovered.StepStatus != turn.StepStatusExecutingTools {
		t.Fatalf("restart projection = %+v", recovered)
	}
	if status, ok := second.turnCoordinator.ToolExecutionStatusForCall("call-restart"); !ok || status != turn.ToolExecutionStatusUnknown {
		t.Fatalf("restart execution status = %s/%v", status, ok)
	}

	if err := second.reconcileToolExecution(context.Background(), recovered.TurnID, recovered.StepID, "call-restart-execution", turn.ToolExecutionStatusSucceeded, "marker already exists"); err != nil {
		t.Fatal(err)
	}
	final := second.turnCoordinator.Snapshot()
	if final.RecoveryRequired || final.StepIndex != 2 || final.StepStatus != turn.StepStatusRequesting {
		t.Fatalf("post-reconciliation projection = %+v", final)
	}
	if second.queue.CountByRequestType(queue.RequestTypeToolResult) != 1 {
		t.Fatalf("expected one resumed tool-result request, queue=%d", second.queue.CountByRequestType(queue.RequestTypeToolResult))
	}
	if len(second.messages) != 1 || second.messages[0].Role != "tool" || second.messages[0].ToolCallID != "call-restart" {
		t.Fatalf("reconciled history = %#v", second.messages)
	}
}

func TestRuntimeLifecycleRecoversCompletedToolResultFromHistory(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "history-recovery.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	first := newLifecycleTestRuntime()
	first.store = st
	first.lifecycleBeginHumanTurn()
	state := first.turnCoordinator.Snapshot()
	first.lifecycleDispatch(turn.TurnCommand{
		Type: turn.CommandAssistantReceived, SessionID: "session-1", TurnID: state.TurnID,
		StepID: state.StepID, Generation: state.Generation, HasTools: true, At: time.Now().UTC(),
	})
	state = first.turnCoordinator.Snapshot()
	first.lifecycleDispatch(turn.TurnCommand{
		Type: turn.CommandToolCallRecorded, SessionID: "session-1", TurnID: state.TurnID,
		StepID: state.StepID, Generation: state.Generation, ToolCallID: "call-history",
		ToolName: "read_file", Arguments: []byte(`{"path":"README.md"}`), At: time.Now().UTC(),
	})
	state = first.turnCoordinator.Snapshot()
	first.lifecycleDispatch(turn.TurnCommand{
		Type: turn.CommandToolExecutionStarted, SessionID: "session-1", TurnID: state.TurnID,
		StepID: state.StepID, Generation: state.Generation, ToolCallID: "call-history",
		ToolExecutionID: "call-history-execution", At: time.Now().UTC(),
	})
	history := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call-history", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"README.md"}`}}}},
		{Role: "tool", ToolCallID: "call-history", Name: "read_file", Content: "README"},
	}
	if err := st.Save(context.Background(), store.Record{AgentID: "session-1", NodeID: "agent-1", Messages: history}); err != nil {
		t.Fatal(err)
	}

	hub := stream.NewHub(16, logx.Discard())
	recovered := newRuntimeWithPublisher(
		"session-1", "agent-1", hub, hub,
		&llm.MockClient{}, nil, nil, st, logx.Discard(), history, nil, nil, 0, nil,
		false, 0, 0, TurnOptions{FSRoot: t.TempDir(), SkillsEnabled: false}, nil,
	)
	state = recovered.turnCoordinator.Snapshot()
	if state.RecoveryRequired || state.StepIndex != 2 || state.StepStatus != turn.StepStatusRequesting {
		t.Fatalf("history recovery projection = %+v", state)
	}
	if recovered.queue.CountByRequestType(queue.RequestTypeToolResult) != 1 {
		t.Fatalf("expected one resumed tool-result request, queue=%d", recovered.queue.CountByRequestType(queue.RequestTypeToolResult))
	}
}
