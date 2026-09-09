package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

type conditionIntegrationLLM struct{ calls atomic.Int32 }

func (c *conditionIntegrationLLM) StreamChat(ctx context.Context, _ llm.ChatRequest, h llm.StreamHandler) (llm.ChatResult, error) {
	c.calls.Add(1)
	if h.OnDelta != nil {
		h.OnDelta("final task completed")
	}
	return llm.ChatResult{Content: "final task completed", FinishReason: "stop"}, nil
}
func (c *conditionIntegrationLLM) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}
func (c *conditionIntegrationLLM) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func TestConditionTriggerHTTPApprovalExecutesOnce(t *testing.T) {
	cfg := triggersTestConfig(t)
	workspace := t.TempDir()
	cfg.RuntimeRoot = workspace
	reg, err := tools.NewRegistry(workspace, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.SetBuiltinEnabled(config.ExpandBuiltinToolGroups([]string{"bash"})); err != nil {
		t.Fatal(err)
	}
	client := &conditionIntegrationLLM{}
	pol := policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"bash_run": policy.ModeAlways}})
	// Seed the registered Agent before NewServer opens its own AgentStore; the
	// HTTP trigger target must pass the real registry lookup, rather than rely
	// on the no-Agent-store test fallback.
	agents, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := agents.Save(context.Background(), store.AgentRecord{
		AgentID: cfg.NodeID, ConfigSnapshot: []byte(`{"agent_type":"normal","defaults":{"tools":{"enabled_groups":["bash"]}}}`),
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		agents.Close()
		t.Fatal(err)
	}
	if err := agents.Close(); err != nil {
		t.Fatal(err)
	}
	// A real server bootstraps node settings from SQLite; seed the completed
	// identity there as well, otherwise BootstrapNodeSettings correctly
	// overlays the test config with the first-run onboarding gate.
	nodeSettings, err := store.OpenNodeSettings(cfg.NodeSettingsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := nodeSettings.Save(context.Background(), cfg); err != nil {
		nodeSettings.Close()
		t.Fatal(err)
	}
	if err := nodeSettings.Close(); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, nil, WithLLM(client), WithTools(reg), WithPolicy(pol))
	t.Cleanup(srv.Close)
	if srv.agents == nil || srv.store == nil {
		t.Fatal("expected registered Agent and SQLite stores")
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	// A fixed target must use the canonical Agent session ID.  A random
	// session would be routed to the canonical runtime by EnsureSessionForAgent
	// and the approval would be persisted on a different session.
	sess, _, err := srv.sessions.Create(cfg.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := sess.ID

	newTrigger := func(name, marker string) string {
		raw, _ := json.Marshal(map[string]any{
			"name": name, "task_template": "final task",
			"condition":       map[string]any{"interval_seconds": 3600, "cmd": fmt.Sprintf("printf approved >> %s", marker)},
			"target_agent_id": cfg.NodeID, "target_session_id": sessionID, "session_target_mode": "fixed",
		})
		resp, e := http.Post(ts.URL+"/v1/triggers", "application/json", bytes.NewReader(raw))
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("create status=%d body=%s", resp.StatusCode, b)
		}
		var out struct {
			TriggerID string `json:"trigger_id"`
		}
		if e := json.NewDecoder(resp.Body).Decode(&out); e != nil {
			t.Fatal(e)
		}
		if out.TriggerID == "" {
			t.Fatal("empty trigger id")
		}
		return out.TriggerID
	}
	fire := func(id string) {
		resp, e := http.Post(ts.URL+"/v1/triggers/"+id+"/fire", "application/json", strings.NewReader(`{"reason":"manual"}`))
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("fire status=%d", resp.StatusCode)
		}
		var out struct {
			Status  string `json:"status"`
			Message string `json:"message"`
		}
		if e := json.NewDecoder(resp.Body).Decode(&out); e != nil {
			t.Fatal(e)
		}
		if out.Status != "awaiting_approval" {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("fire status=%q message=%q body=%s", out.Status, out.Message, b)
		}
	}
	hydrate := func() map[string]any {
		resp, e := http.Get(ts.URL + "/v1/agents/" + sessionID + "/hydrate")
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		var out struct {
			Pending map[string]any `json:"pending_hitl"`
		}
		if e := json.NewDecoder(resp.Body).Decode(&out); e != nil {
			t.Fatal(e)
		}
		return out.Pending
	}
	resume := func(value string) *http.Response {
		body := `{"agent_id":"` + sessionID + `","request_type":"resume","resume_value":` + value + `}`
		req, e := http.NewRequest(http.MethodPost, ts.URL+"/v1/messages", strings.NewReader(body))
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		return resp
	}

	marker := filepath.Join(workspace, "approved.txt")
	id := newTrigger("approval", marker)
	fire(id)
	if client.calls.Load() != 0 {
		t.Fatalf("condition called model: %d", client.calls.Load())
	}
	if p := hydrate(); len(p) == 0 {
		t.Fatal("approval was not persisted")
	}
	bad := resume(`{"unexpected":true}`)
	if bad.StatusCode != http.StatusOK {
		t.Fatalf("malformed resume enqueue status=%d", bad.StatusCode)
	}
	bad.Body.Close()
	time.Sleep(100 * time.Millisecond)
	if p := hydrate(); len(p) == 0 {
		t.Fatal("malformed approval consumed the pending claim")
	}
	if _, e := os.Stat(marker); !os.IsNotExist(e) {
		t.Fatalf("malformed approval executed marker: %v", e)
	}
	ok := resume(`{"type":"approve"}`)
	if ok.StatusCode != http.StatusOK {
		t.Fatalf("approve status=%d", ok.StatusCode)
	}
	ok.Body.Close()
	deadline := time.Now().Add(3 * time.Second)
	var markerContent []byte
	for time.Now().Before(deadline) {
		if raw, e := os.ReadFile(marker); e == nil && string(raw) == "approved" {
			markerContent = raw
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if string(markerContent) != "approved" {
		raw, e := os.ReadFile(marker)
		t.Fatalf("marker=%q err=%v", raw, e)
	}
	modelDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(modelDeadline) && client.calls.Load() < 1 {
		time.Sleep(20 * time.Millisecond)
	}
	if client.calls.Load() != 1 {
		t.Fatalf("final task model calls=%d want 1", client.calls.Load())
	}
	waitSessionIdle(t, srv, sessionID)
	// The final trigger turn can report idle just before its delivery callback
	// releases the Agent gate.  Probe the real gate so the next condition fire
	// is not accidentally testing a transient busy result.
	gateDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(gateDeadline) {
		release, acquired, err := srv.sessions.TryAcquireMaintenance(context.Background(), cfg.NodeID)
		if err == nil && acquired {
			release()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	rejectMarker := filepath.Join(workspace, "rejected.txt")
	rejectID := newTrigger("reject", rejectMarker)
	fire(rejectID)
	resp := resume(`{"type":"reject"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reject status=%d", resp.StatusCode)
	}
	resp.Body.Close()
	time.Sleep(100 * time.Millisecond)
	if _, e := os.Stat(rejectMarker); !os.IsNotExist(e) {
		t.Fatalf("reject executed marker: %v", e)
	}
	if client.calls.Load() != 1 {
		t.Fatalf("reject submitted final task, calls=%d", client.calls.Load())
	}
}
