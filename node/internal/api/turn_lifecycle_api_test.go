package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func TestAgentTimelineSupportsSequenceCursor(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "timeline.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	now := time.Now().UTC()
	for _, event := range []turn.TurnEventEnvelope{
		func() turn.TurnEventEnvelope {
			e := turn.NewTurnEventEnvelope("agent-1", turn.EventTurnStarted, now)
			e.AgentID = "agent-1"
			e.TurnID = "turn-1"
			return e
		}(),
		func() turn.TurnEventEnvelope {
			e := turn.NewTurnEventEnvelope("agent-1", turn.EventStepStarted, now.Add(time.Second))
			e.AgentID = "agent-1"
			e.TurnID = "turn-1"
			e.StepID = "step-1"
			return e
		}(),
	} {
		if _, err := st.AppendTurnEvent(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}

	srv := &Server{store: st}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/agents/{agent_id}/timeline", srv.handleAgentTimeline)
	request := httptest.NewRequest(http.MethodGet, "/v1/agents/agent-1/timeline?limit=1", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("first timeline status=%d body=%s", response.Code, response.Body.String())
	}
	var page turnTimelineResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || page.Events[0].SessionSeq != 1 || page.NextSeq != 1 {
		t.Fatalf("first timeline page = %+v", page)
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/agents/agent-1/timeline?after_seq=1&limit=1", nil)
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("second timeline status=%d body=%s", response.Code, response.Body.String())
	}
	page = turnTimelineResponse{}
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || page.Events[0].EventType != turn.EventStepStarted || page.Events[0].SessionSeq != 2 {
		t.Fatalf("second timeline page = %+v", page)
	}
}
