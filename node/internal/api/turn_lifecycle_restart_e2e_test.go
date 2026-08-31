package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// seedLifecycleEvent writes the minimum durable facts needed to represent a
// process that stopped in the middle of a Turn. The real runtime uses the
// same event envelope; keeping the fixture at this boundary makes the test
// exercise SQLite replay rather than an in-memory shortcut.
func seedLifecycleEvent(
	t *testing.T,
	st *store.SQLiteStore,
	sessionID, turnID, stepID string,
	seq int,
	eventType turn.EventType,
	toolCallID, toolExecutionID, interactionID string,
	meta map[string]any,
) {
	t.Helper()
	payload, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(time.Duration(seq) * time.Millisecond)
	event := turn.NewTurnEventEnvelope(sessionID, eventType, now)
	event.AgentID = sessionID
	event.TurnID = turnID
	event.StepID = stepID
	event.ToolCallID = toolCallID
	event.ToolExecutionID = toolExecutionID
	event.InteractionID = interactionID
	event.SessionSeq = uint64(seq)
	event.TurnSeq = uint64(seq)
	event.CommandID = fmt.Sprintf("seed-lifecycle-%02d", seq)
	event.Source = string(turn.TurnSourceHuman)
	event.Payload = payload
	if _, err := st.AppendTurnEvent(context.Background(), event); err != nil {
		t.Fatalf("append seed event %s: %v", eventType, err)
	}
}

func seedRestartRecord(t *testing.T, st *store.SQLiteStore, sessionID string, messages []llm.Message, pending *turn.PendingHITL) {
	t.Helper()
	if err := st.Save(context.Background(), store.Record{
		AgentID:  sessionID,
		NodeID:   "restart-test-node",
		Messages: messages,
		RuntimeState: store.RuntimeState{
			Pending: pending,
		},
	}); err != nil {
		t.Fatal(err)
	}
}

func newRestartE2EServer(t *testing.T, st *store.SQLiteStore, runtimeRoot string, client llm.Client) (*Server, *httptest.Server) {
	t.Helper()
	cfg := testConfig(t)
	cfg.RuntimeRoot = runtimeRoot
	reg, err := tools.NewRegistry(cfg.RuntimeDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, nil,
		WithLLM(client),
		WithTools(reg),
		WithStore(st),
		WithSkipStore(),
	)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.Close()
		if srv.triggerSched != nil {
			srv.triggerSched.Stop()
		}
		if srv.sessions != nil {
			srv.sessions.Stop()
		}
		// The store is reopened inside the test to model a new process. Close
		// it after all runtime consumers have stopped so Windows can remove the
		// temporary SQLite file reliably.
		_ = st.Close()
	})
	return srv, ts
}

func waitForLifecycleEvent(t *testing.T, st *store.SQLiteStore, sessionID string, eventType turn.EventType) []turn.TurnEventEnvelope {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		events, err := st.ListTurnEvents(context.Background(), sessionID, 0, 300)
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range events {
			if event.EventType == eventType {
				return events
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	events, _ := st.ListTurnEvents(context.Background(), sessionID, 0, 300)
	t.Fatalf("timed out waiting lifecycle event %s; event types=%v", eventType, lifecycleEventTypesForTest(events))
	return nil
}

func lifecycleEventTypesForTest(events []turn.TurnEventEnvelope) []turn.EventType {
	types := make([]turn.EventType, 0, len(events))
	for _, event := range events {
		types = append(types, event.EventType)
	}
	return types
}

func TestRestartRecoveryReconcileThroughHTTP(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	st, err := store.Open(filepath.Join(tmp, "session.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	const sessionID = "restart-reconcile-agent"
	const turnID = "turn-restart-reconcile"
	const stepID = "turn-restart-reconcile-step-1"
	const callID = "call-restart"
	const executionID = callID + "-execution"
	assistant := llm.Message{
		Role: "assistant",
		ToolCalls: []llm.ToolCall{{
			ID: callID, Type: "function",
			Function: llm.ToolCallFunction{Name: "bash", Arguments: `{"command":"printf restart"}`},
		}},
	}
	seedRestartRecord(t, st, sessionID, []llm.Message{
		{Role: "user", Content: "继续执行重启前的工具"},
		assistant,
	}, nil)
	seedLifecycleEvent(t, st, sessionID, turnID, "", 1, turn.EventTurnStarted, "", "", "", map[string]any{
		"generation": 1,
	})
	seedLifecycleEvent(t, st, sessionID, turnID, stepID, 2, turn.EventStepStarted, "", "", "", map[string]any{
		"generation": 1,
	})
	seedLifecycleEvent(t, st, sessionID, turnID, stepID, 3, turn.EventAssistantMessageRecorded, "", "", "", map[string]any{
		"generation": 1,
		"has_tools":  true,
	})
	seedLifecycleEvent(t, st, sessionID, turnID, stepID, 4, turn.EventToolCallRecorded, callID, "", "", map[string]any{
		"generation":     1,
		"tool_name":      "bash",
		"arguments_json": `{"command":"printf restart"}`,
	})
	seedLifecycleEvent(t, st, sessionID, turnID, stepID, 5, turn.EventToolExecutionStarted, callID, executionID, "", map[string]any{
		"generation": 1,
	})

	// Reopen the durable store to model a cold Node process, then expose the
	// actual API handler. Runtime construction must mark the in-flight side
	// effect unknown before the reconcile endpoint can accept a result.
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = store.Open(filepath.Join(tmp, "session.db"))
	if err != nil {
		t.Fatal(err)
	}
	srv, ts := newRestartE2EServer(t, st, tmp, &llm.MockClient{FixedReply: "reconcile finished"})
	if _, _, err := srv.sessions.Create(sessionID); err != nil {
		t.Fatal(err)
	}

	events, err := st.ListTurnEvents(context.Background(), sessionID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	foundUnknown := false
	for _, event := range events {
		if event.EventType == turn.EventToolExecutionFailed && strings.Contains(string(event.Payload), "node_restart_unknown") {
			foundUnknown = true
		}
	}
	if !foundUnknown {
		t.Fatalf("restart did not append unknown execution fact; events=%+v", events)
	}

	body := bytes.NewBufferString(`{"status":"succeeded","content":"recovered after restart"}`)
	request, err := http.NewRequest(http.MethodPost,
		ts.URL+"/v1/agents/"+sessionID+"/turns/"+turnID+"/steps/"+stepID+"/tool-executions/"+executionID+"/reconcile", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	responseBody, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("reconcile status=%d body=%s", response.StatusCode, responseBody)
	}

	waitForLifecycleEvent(t, st, sessionID, turn.EventTurnCompleted)
	waitSessionIdle(t, srv, sessionID)
	events, err = st.ListTurnEvents(context.Background(), sessionID, 0, 200)
	if err != nil {
		t.Fatal(err)
	}
	foundReconciled, foundCompleted := false, false
	for _, event := range events {
		switch event.EventType {
		case turn.EventToolExecutionReconciled:
			foundReconciled = true
		case turn.EventTurnCompleted:
			foundCompleted = true
		}
	}
	if !foundReconciled || !foundCompleted {
		coordinator := turn.NewTurnCoordinator(sessionID, "ops-linux-01")
		restoreErr := coordinator.Restore(events)
		t.Fatalf("reconciled turn did not complete: reconciled=%v completed=%v restore_err=%v snapshot=%+v event_types=%v", foundReconciled, foundCompleted, restoreErr, coordinator.Snapshot(), lifecycleEventTypesForTest(events))
	}
}

func TestRestartRecoveryResumeHITLThroughHTTP(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "session.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	const sessionID = "restart-hitl-agent"
	const turnID = "turn-restart-hitl"
	const stepID = "turn-restart-hitl-step-1"
	const callID = "call-restart-hitl"
	call := llm.ToolCall{
		ID: callID, Type: "function",
		Function: llm.ToolCallFunction{Name: "ask_user_information", Arguments: `{"question":"部署环境是什么？"}`},
	}
	assistant := llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{call}}
	pending := &turn.PendingHITL{Items: []turn.PendingHITLItem{{ToolCall: call}}}
	seedRestartRecord(t, st, sessionID, []llm.Message{
		{Role: "user", Content: "请确认部署环境"},
		assistant,
	}, pending)
	seedLifecycleEvent(t, st, sessionID, turnID, "", 1, turn.EventTurnStarted, "", "", "", map[string]any{
		"generation": 3,
	})
	seedLifecycleEvent(t, st, sessionID, turnID, stepID, 2, turn.EventStepStarted, "", "", "", map[string]any{
		"generation": 3,
	})
	seedLifecycleEvent(t, st, sessionID, turnID, stepID, 3, turn.EventAssistantMessageRecorded, "", "", "", map[string]any{
		"generation": 3,
		"has_tools":  true,
	})
	seedLifecycleEvent(t, st, sessionID, turnID, stepID, 4, turn.EventInteractionRequested, "", "", "interaction-restart", map[string]any{
		"generation":           3,
		"interaction_kind":     "user_information",
		"interaction_revision": 1,
	})

	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	srv, ts := newRestartE2EServer(t, st, tmp, &llm.MockClient{FixedReply: "HITL resumed"})
	if _, _, err := srv.sessions.Create(sessionID); err != nil {
		t.Fatal(err)
	}

	body := bytes.NewBufferString(`{"request_type":"resume","agent_id":"` + sessionID + `","resume_value":{"type":"user_information","tool_call_id":"` + callID + `","answer":"staging"}}`)
	response, err := http.Post(ts.URL+"/v1/messages", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	responseBody, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("resume status=%d body=%s", response.StatusCode, responseBody)
	}

	waitForLifecycleEvent(t, st, sessionID, turn.EventInteractionResolved)
	waitForLifecycleEvent(t, st, sessionID, turn.EventTurnCompleted)
	waitSessionIdle(t, srv, sessionID)
	events, err := st.ListTurnEvents(context.Background(), sessionID, 0, 200)
	if err != nil {
		t.Fatal(err)
	}
	foundResolved, foundCompleted := false, false
	for _, event := range events {
		switch event.EventType {
		case turn.EventInteractionResolved:
			foundResolved = true
		case turn.EventTurnCompleted:
			foundCompleted = true
		}
	}
	if !foundResolved || !foundCompleted {
		t.Fatalf("resumed HITL turn did not complete: resolved=%v completed=%v event_types=%v", foundResolved, foundCompleted, lifecycleEventTypesForTest(events))
	}
}
