package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

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
		RuntimeState: store.RuntimeState{
			HistoryRevision: 7,
			Pending: &turn.PendingHITL{Items: []turn.PendingHITLItem{{
				ToolCall: llm.ToolCall{
					ID:   "call-1",
					Type: "function",
					Function: llm.ToolCallFunction{
						Name:      "bash_run",
						Arguments: `{"command":"echo x"}`,
					},
				},
			}}},
		},
	}); err != nil {
		t.Fatal(err)
	}

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
	if got.RunTurnPhase != "awaiting_hitl" {
		t.Fatalf("run_turn_phase = %q", got.RunTurnPhase)
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
