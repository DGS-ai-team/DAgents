package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestSessionContextFullMessages(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()

	sessionID := createTestRuntime(t, srv)

	ctxResp, err := http.Get(ts.URL + "/v1/agents/" + sessionID + "/context?full_messages=1")
	if err != nil {
		t.Fatal(err)
	}
	defer ctxResp.Body.Close()
	if ctxResp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", ctxResp.StatusCode)
	}
	var body struct {
		MessagesCount  int                     `json:"messages_count"`
		Messages       []contextMessagePreview `json:"messages"`
		RecentMessages []contextMessagePreview `json:"recent_messages"`
	}
	if err := json.NewDecoder(ctxResp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Messages == nil {
		t.Fatal("expected messages field with full_messages=1")
	}
	if len(body.Messages) != body.MessagesCount {
		t.Fatalf("messages len = %d count = %d", len(body.Messages), body.MessagesCount)
	}
}
