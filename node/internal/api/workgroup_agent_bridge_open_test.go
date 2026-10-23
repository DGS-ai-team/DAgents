package api

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/memory"
	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/workgroup"
)

func newWorkgroupOpenTestServer(t *testing.T) (*Server, *workgroupAgentBridge) {
	t.Helper()
	cfg := testConfig(t)
	cfg.LLM.Mock = true
	history, err := store.Open(cfg.SessionDBPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := history.Save(context.Background(), store.Record{
		AgentID:  "wg-session-test",
		Messages: []llm.Message{{Role: "user", Content: "keep this workgroup history"}},
	}); err != nil {
		t.Fatal(err)
	}
	agents, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithStore(history), WithSkipStore())
	if srv.triggerSched != nil {
		srv.triggerSched.Stop()
	}
	srv.agents = agents
	t.Cleanup(func() {
		srv.sessions.Stop()
		_ = history.Close()
		_ = agents.Close()
	})
	if err := agents.Save(context.Background(), store.AgentRecord{
		AgentID:        "agt-workgroup-open",
		DisplayName:    "Workgroup test",
		ConfigSnapshot: json.RawMessage(`{"defaults":{"tools":{"enabled_groups":["fs"]}}}`),
	}); err != nil {
		t.Fatal(err)
	}
	return srv, newWorkgroupAgentBridge(srv)
}

func workgroupOpenRequest() workgroup.AgentSessionOpenRequest {
	return workgroup.AgentSessionOpenRequest{
		WorkgroupID: "wg-test",
		MemberID:    "member-test",
		AgentID:     "agt-workgroup-open",
		SessionID:   "wg-session-test",
	}
}

func TestWorkgroupOpenAgentSessionReusesBindingAndRegistry(t *testing.T) {
	srv, bridge := newWorkgroupOpenTestServer(t)
	ctx := context.Background()
	req := workgroupOpenRequest()
	first, err := bridge.OpenAgentSession(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != "ready" {
		t.Fatalf("first open status = %q, want ready", first.Status)
	}
	registry := bridge.registryForSession(req.SessionID)
	if registry == nil || srv.sessions.SessionTools(req.SessionID) != registry {
		t.Fatal("new workgroup session must use its registered scoped registry")
	}

	const retries = 6
	start := make(chan struct{})
	errs := make(chan error, retries)
	var wg sync.WaitGroup
	for i := 0; i < retries; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, openErr := bridge.OpenAgentSession(ctx, req)
			if openErr == nil && result.Status != "ready" {
				openErr = errors.New("retry status = " + result.Status)
			}
			errs <- openErr
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := bridge.registryForSession(req.SessionID); got != registry {
		t.Fatal("repeated open replaced the session registry")
	}
	if got := srv.sessions.SessionTools(req.SessionID); got != registry {
		t.Fatal("repeated open changed the registry used by the live manager runtime")
	}

	conflicting := req
	conflicting.WorkgroupID = "wg-other"
	if _, err := bridge.OpenAgentSession(ctx, conflicting); err == nil {
		t.Fatal("open with a different workgroup binding should fail")
	}
	if got := bridge.registryForSession(req.SessionID); got != registry {
		t.Fatal("conflicting open replaced the original session registry")
	}
}

func TestWorkgroupOpenRejectsUnknownLiveRuntimeAndKeepsItsRegistry(t *testing.T) {
	srv, bridge := newWorkgroupOpenTestServer(t)
	req := workgroupOpenRequest()
	original, err := tools.NewRegistry(t.TempDir(), 10, "utf-8", "utf-8")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := srv.sessions.CreateWithOptionsAndLLM(req.SessionID, session.TurnOptions{}, original, nil, &llm.MockClient{}, req.AgentID); err != nil {
		t.Fatal(err)
	}
	if _, err := bridge.OpenAgentSession(context.Background(), req); err == nil {
		t.Fatal("open should reject a live session without a matching bridge binding")
	}
	if got := srv.sessions.SessionTools(req.SessionID); got != original {
		t.Fatal("failed open replaced the pre-existing runtime registry")
	}
	if got := bridge.registryForSession(req.SessionID); got != nil {
		t.Fatal("failed open must not publish a registry for the unknown runtime")
	}
}

func TestWorkgroupOpenRebuildsAfterManagerReleasedRuntime(t *testing.T) {
	srv, bridge := newWorkgroupOpenTestServer(t)
	req := workgroupOpenRequest()
	if _, err := bridge.OpenAgentSession(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	oldRegistry := bridge.registryForSession(req.SessionID)
	if released, err := srv.sessions.Release(req.SessionID); err != nil || !released {
		t.Fatalf("release session = (%v, %v), want (true, nil)", released, err)
	}
	if _, err := bridge.OpenAgentSession(context.Background(), req); err != nil {
		t.Fatalf("reopen released session: %v", err)
	}
	newRegistry := bridge.registryForSession(req.SessionID)
	if newRegistry == nil || newRegistry == oldRegistry {
		t.Fatal("reopen after release should build and register a fresh registry")
	}
	if got := srv.sessions.SessionTools(req.SessionID); got != newRegistry {
		t.Fatal("restored manager runtime does not use the newly registered registry")
	}
}

func TestWorkgroupCloseRejectsBusyAndReopenRestoresHistory(t *testing.T) {
	srv, bridge := newWorkgroupOpenTestServer(t)
	ctx := context.Background()
	req := workgroupOpenRequest()
	if _, err := bridge.OpenAgentSession(ctx, req); err != nil {
		t.Fatal(err)
	}
	firstRegistry := bridge.registryForSession(req.SessionID)
	_, before, err := srv.sessions.ContextSummary(req.SessionID)
	if err != nil || len(before) != 1 || before[0].Content != "keep this workgroup history" {
		t.Fatalf("initial history = (%+v, %v), want seeded message", before, err)
	}

	bridge.mu.Lock()
	binding := bridge.bindings[req.SessionID]
	bridge.turns["busy-assign"] = workgroupAgentTurn{binding: binding}
	bridge.mu.Unlock()
	if err := bridge.CloseAgentSession(ctx, req); err == nil {
		t.Fatal("closing a session with an active workgroup turn should fail")
	}
	if bridge.registryForSession(req.SessionID) != firstRegistry {
		t.Fatal("busy close must retain the active binding and registry")
	}

	bridge.mu.Lock()
	delete(bridge.turns, "busy-assign")
	bridge.mu.Unlock()
	if err := bridge.CloseAgentSession(ctx, req); err != nil {
		t.Fatalf("close idle session: %v", err)
	}
	if _, _, _, err := srv.sessions.RuntimeInfo(req.SessionID); err == nil {
		t.Fatal("close should release the idle runtime from memory")
	}
	if bridge.registryForSession(req.SessionID) != nil {
		t.Fatal("closed session should no longer expose its scoped registry")
	}
	if _, err := bridge.OpenAgentSession(ctx, req); err != nil {
		t.Fatalf("reopen after close: %v", err)
	}
	newRegistry := bridge.registryForSession(req.SessionID)
	if newRegistry == nil || newRegistry == firstRegistry || srv.sessions.SessionTools(req.SessionID) != newRegistry {
		t.Fatal("reopened session should bind a fresh registry to the restored runtime")
	}
	_, after, err := srv.sessions.ContextSummary(req.SessionID)
	if err != nil || len(after) != 1 || after[0].Content != "keep this workgroup history" {
		t.Fatalf("reopened history = (%+v, %v), want original message", after, err)
	}
}

func TestUntransferredAgentRuntimeIsClosed(t *testing.T) {
	closer := &memoryServiceCloser{}
	owner := agentSessionRuntimeOwnership{built: agentruntime.Built{TurnOptions: session.TurnOptions{MemoryService: closer}}}
	owner.closeIfUntransferred()
	if closer.closed != 1 {
		t.Fatalf("untransferred runtime close count = %d, want 1", closer.closed)
	}

	transferred := &memoryServiceCloser{}
	owner = agentSessionRuntimeOwnership{built: agentruntime.Built{TurnOptions: session.TurnOptions{MemoryService: transferred}}, transferred: true}
	owner.closeIfUntransferred()
	if transferred.closed != 0 {
		t.Fatalf("transferred runtime close count = %d, want 0", transferred.closed)
	}
}

type memoryServiceCloser struct{ closed int }

func (m *memoryServiceCloser) Recall(context.Context, memory.RecallRequest) (memory.Snapshot, error) {
	return memory.Snapshot{}, nil
}
func (m *memoryServiceCloser) Search(context.Context, memory.SearchRequest) ([]memory.SearchResult, error) {
	return nil, nil
}
func (m *memoryServiceCloser) Get(context.Context, memory.Scope, string, bool) (memory.Entry, error) {
	return memory.Entry{}, nil
}
func (m *memoryServiceCloser) Remember(context.Context, memory.RememberRequest) (memory.WriteResult, error) {
	return memory.WriteResult{}, nil
}
func (m *memoryServiceCloser) Forget(context.Context, memory.Scope, string, string) (memory.WriteResult, error) {
	return memory.WriteResult{}, nil
}
func (m *memoryServiceCloser) ResolveConflict(context.Context, memory.Scope, string, memory.ConflictDecision) (memory.WriteResult, error) {
	return memory.WriteResult{}, nil
}
func (m *memoryServiceCloser) SetScope(memory.Scope) {}
func (m *memoryServiceCloser) Close() error {
	m.closed++
	return nil
}
