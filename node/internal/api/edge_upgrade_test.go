package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExtractEdgeAgentID_FromPath(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/agents/agt-1/hydrate", nil)
	id, body, ok := extractEdgeAgentID(req)
	if !ok || id != "agt-1" || body != nil {
		t.Fatalf("got id=%q ok=%v body=%v", id, ok, body != nil)
	}
}

func TestExtractEdgeAgentID_FromStreamsQuery(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/streams?agent_id=agt-9", nil)
	id, _, ok := extractEdgeAgentID(req)
	if !ok || id != "agt-9" {
		t.Fatalf("got id=%q ok=%v", id, ok)
	}
}

func TestExtractEdgeAgentID_FromMessagesBody(t *testing.T) {
	raw := []byte(`{"agent_id":"agt-msg","text":"hi"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(raw))
	id, body, ok := extractEdgeAgentID(req)
	if !ok || id != "agt-msg" {
		t.Fatalf("got id=%q ok=%v", id, ok)
	}
	if string(body) != string(raw) {
		t.Fatalf("body not restored")
	}
}

func TestExtractEdgeAgentID_SkipCreateAndList(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/agents", strings.NewReader(`{}`))
	if _, _, ok := extractEdgeAgentID(req); ok {
		t.Fatal("create should not extract")
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/agents", nil)
	if _, _, ok := extractEdgeAgentID(req); ok {
		t.Fatal("list should not extract")
	}
}

func TestIsExactAgentRootPath(t *testing.T) {
	if !isExactAgentRootPath("/v1/agents/agt-1", "agt-1") {
		t.Fatal("expected root match")
	}
	if isExactAgentRootPath("/v1/agents/agt-1/hydrate", "agt-1") {
		t.Fatal("subpath should not match")
	}
}
