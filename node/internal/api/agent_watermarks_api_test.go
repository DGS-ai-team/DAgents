package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAgentWatermarksReturnsLightweightActiveRevisionAndAgentCursor(t *testing.T) {
	srv, _ := newTestServer(t)
	sessionID := createTestRuntime(t, srv)
	srv.stream.Publish(sessionID, "test_event", map[string]any{"ignored": true})

	req := httptest.NewRequest(http.MethodGet, "/v1/agents/watermarks", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response agentWatermarksResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.StreamEpoch == "" || len(response.Agents) != 1 {
		t.Fatalf("response=%+v", response)
	}
	item := response.Agents[0]
	if item.AgentID != sessionID || item.ConversationID != sessionID || item.LatestAgentSeq != 1 || item.ResultCommitStatus != "unknown" {
		t.Fatalf("watermark=%+v", item)
	}
	if item.ConversationRevision != 0 {
		t.Fatalf("new session watermark=%+v, want empty revisions", item)
	}
}
