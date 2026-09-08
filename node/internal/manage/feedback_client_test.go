package manage

import (
	"context"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/shared/config"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func feedbackControl(raw string) *ControlClient {
	return NewControlClient(&config.Config{NodeID: "node/a", Manage: config.ManageConfig{Enabled: true, URL: raw, NodeToken: "secret"}})
}
func TestFeedbackClientRejectsRedirectAndSendsMinimalPayload(t *testing.T) {
	redirects := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirects++; http.Error(w, "must not reach", 500) }))
	defer target.Close()
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/v1/feedback", http.StatusFound)
	}))
	defer src.Close()
	c := NewFeedbackClient(feedbackControl(src.URL))
	err := c.Submit(context.Background(), store.Feedback{ClientFeedbackID: "id", NodeID: "node/a", Category: "bug", Title: "t", Body: "b", Destination: "leak", DeliveryStatus: "delivered"})
	if err == nil || redirects != 0 {
		t.Fatalf("redirect err=%v target_count=%d", err, redirects)
	}
}
func TestFeedbackClientEscapesIDAndValidatesResponse(t *testing.T) {
	var path, rawPath, query string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, rawPath, query = r.URL.Path, r.URL.RawPath, r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"client_feedback_id":"wrong","node_id":"node/a"}`))
	}))
	defer s.Close()
	c := NewFeedbackClient(feedbackControl(s.URL))
	if _, e := c.Get(context.Background(), "x/y"); e == nil {
		t.Fatal("expected mismatched response id")
	}
	if rawPath == "" || !strings.Contains(rawPath, "%2F") {
		t.Fatalf("id was not escaped: path=%q raw=%q", path, rawPath)
	}
	if query != "node_id=node%2Fa" {
		t.Fatalf("query not escaped: %q", query)
	}
}
func TestFeedbackClientRejectsInvalidDestination(t *testing.T) {
	for _, raw := range []string{"https://u:p@example.invalid", "https://example.invalid/?x=1", "https://example.invalid/#f"} {
		c := NewFeedbackClient(feedbackControl(raw))
		if err := c.Submit(context.Background(), store.Feedback{ClientFeedbackID: "id", Category: "bug", Title: "t", Body: "b"}); err == nil {
			t.Fatalf("destination accepted: %s", raw)
		}
	}
}
