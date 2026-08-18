package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

func TestHandleAgentExecutionEvents(t *testing.T) {
	cfg := testConfig(t)
	st, err := store.Open(filepath.Join(cfg.FSRoot, "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, nil, WithStore(st), WithSkipStore())
	t.Cleanup(srv.Close)
	if err := st.Save(context.Background(), store.Record{AgentID: "agent-events-api"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendExecutionEvent(context.Background(), store.ExecutionEventRecord{
		AgentID:    "agent-events-api",
		SessionID:  "agent-events-api",
		ProcessID:  "local-process-1",
		ProcessSeq: 1,
		EventType:  "process_started",
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/agents/agent-events-api/execution-events?limit=10", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		AgentID string                   `json:"agent_id"`
		Events  []executionEventResponse `json:"events"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.AgentID != "agent-events-api" || len(body.Events) != 1 {
		t.Fatalf("body=%+v", body)
	}
	if body.Events[0].EventType != "process_started" || body.Events[0].ProcessSeq != 1 {
		t.Fatalf("event=%+v", body.Events[0])
	}
}
