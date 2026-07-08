package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func TestSessionContextFullMessages(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	createResp, err := http.Post(ts.URL+"/v1/sessions", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	var created createSessionResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	createResp.Body.Close()
	if created.SessionID == "" {
		t.Fatal("empty session_id")
	}

	ctxResp, err := http.Get(ts.URL + "/v1/sessions/" + created.SessionID + "/context?full_messages=1")
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
