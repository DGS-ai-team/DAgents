package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

func TestManualCompressionPersistenceFailureReturnsAPIError(t *testing.T) {
	cfg := testConfig(t)
	storePath := filepath.Join(cfg.RuntimeRoot, "compression-api.db")
	st, err := store.Open(storePath)
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithStore(st), WithSkipStore())
	t.Cleanup(srv.Close)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	sessionID := createTestRuntime(t, srv)
	messageBody := testIdempotentMessageBody(t, sessionID, "请基于这段内容压缩上下文："+string(bytes.Repeat([]byte("会话原文。"), 80)))
	messageResp, err := http.Post(ts.URL+"/v1/messages", "application/json", bytes.NewReader(messageBody))
	if err != nil {
		t.Fatal(err)
	}
	messageResp.Body.Close()
	if messageResp.StatusCode != http.StatusAccepted && messageResp.StatusCode != http.StatusOK {
		t.Fatalf("message status = %d, want accepted", messageResp.StatusCode)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		contextResp, err := http.Get(ts.URL + "/v1/agents/" + sessionID + "/context")
		if err != nil {
			t.Fatal(err)
		}
		var snapshot sessionContextResponse
		decodeErr := json.NewDecoder(contextResp.Body).Decode(&snapshot)
		contextResp.Body.Close()
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if snapshot.MessagesCount >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for message history; count=%d", snapshot.MessagesCount)
		}
		time.Sleep(20 * time.Millisecond)
	}
	waitSessionIdle(t, srv, sessionID)

	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	compressResp, err := http.Post(ts.URL+"/v1/agents/"+sessionID+"/compress", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer compressResp.Body.Close()
	var got errorBody
	if err := json.NewDecoder(compressResp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if compressResp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("compress status = %d, want %d; body=%+v", compressResp.StatusCode, http.StatusInternalServerError, got)
	}
	if got.Error.Code != "compression_persistence_failed" || got.Error.Details["status"] != "persistence_failed" {
		t.Fatalf("compress error = %+v, want structured persistence failure", got.Error)
	}
}
