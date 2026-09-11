package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func TestHandleSessionHydrate(t *testing.T) {
	cfg := testConfig(t)
	dbPath := filepath.Join(cfg.RuntimeDir(), "sessions.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	srv := NewServer(cfg, nil, WithStore(st), WithSkipStore())
	t.Cleanup(srv.Close)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	sessionID := "sess-hydrate-test"
	if err := st.Save(context.Background(), store.Record{
		AgentID: sessionID,
		Messages: []llm.Message{
			{Role: "user", Content: "ping"},
			{Role: "assistant", Content: "pong"},
		},
		RuntimeState: store.RuntimeState{HistoryRevision: 7},
	}); err != nil {
		t.Fatal(err)
	}
	pending := turn.PendingHITL{Items: []turn.PendingHITLItem{{
		ToolCall: llm.ToolCall{
			ID:   "call-1",
			Type: "function",
			Function: llm.ToolCallFunction{
				Name:      "bash_run",
				Arguments: `{"command":"echo x"}`,
			},
		},
	}}}
	payload, err := json.Marshal(pending)
	if err != nil {
		t.Fatal(err)
	}
	appendEvent := func(eventType turn.EventType, seq int, meta map[string]any) {
		t.Helper()
		event := turn.NewTurnEventEnvelope(sessionID, eventType, time.Now().UTC().Add(time.Duration(seq)*time.Millisecond))
		event.AgentID = sessionID
		event.TurnID = "turn-hydrate-test"
		event.StepID = "step-hydrate-test"
		event.InteractionID = "interaction-hydrate-test"
		event.SessionSeq = uint64(seq)
		event.TurnSeq = uint64(seq)
		event.Payload, err = json.Marshal(meta)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.AppendTurnEvent(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	appendEvent(turn.EventTurnStarted, 1, map[string]any{"generation": 1})
	appendEvent(turn.EventStepStarted, 2, map[string]any{"generation": 1})
	appendEvent(turn.EventAssistantMessageRecorded, 3, map[string]any{"generation": 1, "has_tools": true})
	appendEvent(turn.EventToolBatchCreated, 4, map[string]any{"generation": 1, "tool_batch_id": "batch-hydrate-test"})
	appendEvent(turn.EventInteractionRequested, 5, map[string]any{
		"interaction_kind":     "approval",
		"interaction_revision": 1,
		"interaction_payload":  json.RawMessage(payload),
	})

	resp, err := http.Get(ts.URL + "/v1/agents/" + sessionID + "/hydrate")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d body = %s", resp.StatusCode, body)
	}
	var got sessionHydrateResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.AgentID != sessionID {
		t.Fatalf("agent_id = %q", got.AgentID)
	}
	if got.TurnState.Phase != "tool_waiting" {
		t.Fatalf("turn_state.phase = %q", got.TurnState.Phase)
	}
	if len(got.Transcript) != 2 {
		t.Fatalf("transcript len = %d", len(got.Transcript))
	}
	if got.HistoryRevision != 7 || got.TurnState.HistoryRevision != 7 {
		t.Fatalf("history revision = response:%d turn:%d", got.HistoryRevision, got.TurnState.HistoryRevision)
	}
	if got.PendingHITL == nil || got.PendingHITL["hitl_id"] == "" {
		t.Fatalf("pending_hitl = %#v", got.PendingHITL)
	}
}

func TestHandleSessionHydrate_notFound(t *testing.T) {
	srv := NewServer(testConfig(t), nil, WithSkipStore())
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/v1/agents/missing/hydrate")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}
