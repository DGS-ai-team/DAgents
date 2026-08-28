package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

func TestHandleSessionAck(t *testing.T) {
	cfg := testConfig(t)
	dbPath := filepath.Join(cfg.FSRoot, "sessions.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	sessionID := "sess-ack-test"
	if err := st.Save(context.Background(), store.Record{
		AgentID:  sessionID,
		Messages: []llm.Message{{Role: "user", Content: "hi"}},
		RuntimeState: store.RuntimeState{
			NotifySeq: 42,
			AckSeq:    10,
		},
	}); err != nil {
		t.Fatal(err)
	}

	srv := NewServer(cfg, nil, WithStore(st), WithSkipStore())
	t.Cleanup(srv.Close)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	body, _ := json.Marshal(map[string]any{"agent_seq": 42})
	resp, err := http.Post(ts.URL+"/v1/agents/"+sessionID+"/ack", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d body = %s", resp.StatusCode, raw)
	}
	var got sessionAckResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.AgentID != sessionID || got.NotifySeq != 42 || got.AckSeq != 42 || got.HasUnread {
		t.Fatalf("got = %+v", got)
	}
}
