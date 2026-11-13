package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/workgroup"
)

func workgroupHasTool(registry *tools.Registry, name string) bool {
	for _, def := range registry.Definitions() {
		if def.Function.Name == name {
			return true
		}
	}
	return false
}

func waitWorkgroupTurnIdle(t *testing.T, srv *Server, bridge *workgroupAgentBridge, sessionID, assignID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, active, _, runtimeErr := srv.sessions.RuntimeInfo(sessionID)
		bridge.mu.Lock()
		_, turnRunning := bridge.turns[assignID]
		bridge.mu.Unlock()
		if runtimeErr == nil && !active && !turnRunning {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("workgroup turn %q did not become idle", assignID)
}

func TestWorkgroupWebRuntimeRefreshesAtNextIdleTurn(t *testing.T) {
	var authorizations []string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorizations = append(authorizations, r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"results":[{"title":"T","url":"https://example.com","snippet":"S","summary":"M","content":"body"}]}`))
	}))
	defer provider.Close()

	srv, bridge := newWorkgroupOpenTestServer(t)
	req := workgroupOpenRequest()
	record, err := srv.agents.Get(context.Background(), req.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	record.ConfigSnapshot = json.RawMessage(`{"defaults":{"tools":{"enabled_groups":["web_search"]}}}`)
	if err := srv.agents.Save(context.Background(), *record); err != nil {
		t.Fatal(err)
	}
	if _, err := bridge.OpenAgentSession(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	registry := bridge.registryForSession(req.SessionID)
	if registry == nil {
		t.Fatal("workgroup registry was not created")
	}
	if workgroupHasTool(registry, "web_search") {
		t.Fatal("web_search visible while globally disabled")
	}
	_, historyBefore, err := srv.sessions.ContextSummary(req.SessionID)
	if err != nil || len(historyBefore) != 1 || historyBefore[0].Content != "keep this workgroup history" {
		t.Fatalf("initial history = (%+v, %v)", historyBefore, err)
	}

	setWebConfig := func(enabled bool, key string) {
		srv.llmRefMu.Lock()
		srv.cfg.Web.SearchEnabled = enabled
		srv.cfg.Web.BaseURL = provider.URL
		srv.cfg.Web.APIKey = key
		bridge.markWebRefreshPending()
		srv.llmRefMu.Unlock()
	}
	start := func(assignID string) {
		t.Helper()
		err := bridge.StartAgentTurn(context.Background(), workgroup.AgentTurnStartRequest{
			WorkgroupID: req.WorkgroupID,
			MemberID:    req.MemberID,
			AgentID:     req.AgentID,
			SessionID:   req.SessionID,
			AssignID:    assignID,
			Source:      "direct_member",
			ChildTurnID: "child-" + assignID,
			AttemptID:   "attempt-" + assignID,
			UserMessage: "hello",
		})
		if err != nil {
			t.Fatalf("start workgroup turn %q: %v", assignID, err)
		}
	}
	search := func(wantAuthorization string) {
		t.Helper()
		if _, err := registry.Execute(context.Background(), "web_search", `{"query":"test","call_purpose":"verify runtime refresh"}`); err != nil {
			t.Fatalf("execute web_search: %v", err)
		}
		if got := authorizations[len(authorizations)-1]; got != wantAuthorization {
			t.Fatalf("provider authorization = %q, want %q", got, wantAuthorization)
		}
	}

	setWebConfig(true, "synthetic-workgroup-key-one")
	start("web-on")
	if !workgroupHasTool(registry, "web_search") {
		t.Fatal("web_search did not appear on the next idle turn")
	}
	waitWorkgroupTurnIdle(t, srv, bridge, req.SessionID, "web-on")
	search("Bearer synthetic-workgroup-key-one")

	setWebConfig(true, "synthetic-workgroup-key-two")
	start("key-rotated")
	waitWorkgroupTurnIdle(t, srv, bridge, req.SessionID, "key-rotated")
	search("Bearer synthetic-workgroup-key-two")

	setWebConfig(false, "")
	start("web-off")
	if workgroupHasTool(registry, "web_search") {
		t.Fatal("web_search remained visible after disabling it at an idle turn boundary")
	}
	waitWorkgroupTurnIdle(t, srv, bridge, req.SessionID, "web-off")
	if _, err := registry.Execute(context.Background(), "web_search", `{"query":"test"}`); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("disabled web_search execution error = %v", err)
	}
	if got := bridge.registryForSession(req.SessionID); got != registry || srv.sessions.SessionTools(req.SessionID) != registry {
		t.Fatal("web refresh replaced the session registry")
	}
	_, historyAfter, err := srv.sessions.ContextSummary(req.SessionID)
	if err != nil || len(historyAfter) <= len(historyBefore) || historyAfter[0].Content != historyBefore[0].Content {
		t.Fatalf("web refresh changed or lost workgroup history: (%+v, %v)", historyAfter, err)
	}
}
