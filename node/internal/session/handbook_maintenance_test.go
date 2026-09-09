package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/handbookfs"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

type handbookRoundClient struct {
	calls    int
	requests []llm.ChatRequest
}

type handbookAskClient struct{}

func (*handbookAskClient) StreamChat(context.Context, llm.ChatRequest, llm.StreamHandler) (llm.ChatResult, error) {
	return llm.ChatResult{ToolCalls: []llm.ToolCall{{ID: "ask", Type: "function", Function: llm.ToolCallFunction{Name: "ask_user_information", Arguments: `{"question":"confirm"}`}}}, FinishReason: "tool_calls"}, nil
}
func (*handbookAskClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}
func (*handbookAskClient) NormalizeAssistant(e []llm.Message, m llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(e, m)
}

type handbookBlockingClient struct{ started chan struct{} }

type handbookNoopClient struct {
	store      *store.SQLiteStore
	closeStore bool
	calls      int
}

func (c *handbookNoopClient) StreamChat(_ context.Context, _ llm.ChatRequest, _ llm.StreamHandler) (llm.ChatResult, error) {
	c.calls++
	if c.closeStore && c.store != nil {
		_ = c.store.Close()
		c.store = nil
	}
	if c.calls == 1 {
		return llm.ChatResult{ToolCalls: []llm.ToolCall{{ID: "noop-write", Type: "function", Function: llm.ToolCallFunction{Name: "write_file", Arguments: `{"path":"handbook/guide.md","content":"stable","call_purpose":"maintain"}`}}}, FinishReason: "tool_calls"}, nil
	}
	return llm.ChatResult{Content: "done", FinishReason: "stop"}, nil
}
func (*handbookNoopClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}
func (*handbookNoopClient) NormalizeAssistant(e []llm.Message, m llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(e, m)
}

func (c *handbookBlockingClient) StreamChat(ctx context.Context, _ llm.ChatRequest, _ llm.StreamHandler) (llm.ChatResult, error) {
	close(c.started)
	<-ctx.Done()
	return llm.ChatResult{}, ctx.Err()
}
func (*handbookBlockingClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}
func (*handbookBlockingClient) NormalizeAssistant(e []llm.Message, m llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(e, m)
}

func (c *handbookRoundClient) StreamChat(ctx context.Context, request llm.ChatRequest, h llm.StreamHandler) (llm.ChatResult, error) {
	c.calls++
	c.requests = append(c.requests, request)
	if h.OnUsage != nil {
		h.OnUsage(llm.Usage{PromptTokens: 2, CompletionTokens: 2, TotalTokens: 4})
	}
	if c.calls == 1 {
		return llm.ChatResult{ToolCalls: []llm.ToolCall{{ID: "read", Type: "function", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"handbook/guide.md"}`}}}, FinishReason: "tool_calls"}, nil
	}
	if c.calls == 2 {
		return llm.ChatResult{Content: "done", FinishReason: "stop"}, nil
	}
	return llm.ChatResult{Content: "done", FinishReason: "stop"}, nil
}
func (*handbookRoundClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}
func (*handbookRoundClient) NormalizeAssistant(e []llm.Message, m llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(e, m)
}

type handbookEditClient struct {
	calls         int
	noUsageSecond bool
}

func (c *handbookEditClient) StreamChat(_ context.Context, _ llm.ChatRequest, h llm.StreamHandler) (llm.ChatResult, error) {
	c.calls++
	if h.OnUsage != nil && !(c.noUsageSecond && c.calls == 2) {
		h.OnUsage(llm.Usage{PromptTokens: 2, CompletionTokens: 2, TotalTokens: 4})
	}
	var tc llm.ToolCall
	switch c.calls {
	case 1:
		tc = llm.ToolCall{ID: "w", Type: "function", Function: llm.ToolCallFunction{Name: "write_file", Arguments: `{"path":"handbook/a/b/c.md","content":"old","call_purpose":"maintain"}`}}
	case 2:
		tc = llm.ToolCall{ID: "r", Type: "function", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"handbook/a/b/c.md","call_purpose":"maintain"}`}}
	case 3:
		tc = llm.ToolCall{ID: "s", Type: "function", Function: llm.ToolCallFunction{Name: "search_replace", Arguments: `{"path":"handbook/a/b/c.md","old_string":"old","new_string":"new","call_purpose":"maintain"}`}}
	default:
		return llm.ChatResult{Content: "done", FinishReason: "stop"}, nil
	}
	return llm.ChatResult{ToolCalls: []llm.ToolCall{tc}, FinishReason: "tool_calls"}, nil
}
func (*handbookEditClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}
func (*handbookEditClient) NormalizeAssistant(e []llm.Message, m llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(e, m)
}

func TestHandbookMaintenanceNoopPersistsExternalFact(t *testing.T) {
	workspace, handbook := t.TempDir(), t.TempDir()
	reg, _ := tools.NewRegistry(workspace, 30)
	_ = reg.SetBuiltinEnabled(config.ExpandBuiltinToolGroups([]string{"filesystem"}))
	if err := reg.SetHandbookRoot(handbook); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(handbook, "guide.md"), []byte("stable"), 0644); err != nil {
		t.Fatal(err)
	}
	turnStore, err := store.Open(filepath.Join(workspace, "turn-events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer turnStore.Close()
	client := &handbookNoopClient{}
	allow := policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"write_file": policy.ModeNever}})
	mgr := NewManager("agent-1", stream.NewHub(16, logx.Discard()), client, reg, allow, turnStore, TurnOptions{AutoAgent: true}, logx.Discard())
	defer mgr.Stop()
	rt, _, err := mgr.CreateWithOptionsAndLLM("maintenance-noop", TurnOptions{AutoAgent: true}, reg, nil, client, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	leaseCtx, release, acquired, err := mgr.TryAcquireMaintenanceContext(context.Background(), "agent-1")
	if err != nil || !acquired {
		t.Fatalf("gate: %v %v", acquired, err)
	}
	defer release()
	var boundSession, boundTurn string
	leaseCtx = handbookfs.WithProvenance(leaseCtx, handbookfs.Provenance{MaintenanceReceiptID: "receipt-noop", SessionID: rt.ID})
	result, err := mgr.RunHandbookMaintenanceWithBinding(leaseCtx, rt.ID, "整理手册", turn.TurnBudget{MaxSteps: 2, MaxTotalTokens: 100}, func(sessionID, turnID string) error { boundSession, boundTurn = sessionID, turnID; return nil })
	if err != nil || result.Changed {
		t.Fatalf("noop result=%+v err=%v", result, err)
	}
	events, err := turnStore.ListTurnEventsForTurn(context.Background(), boundSession, boundTurn)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	factCount := 0
	for _, event := range events {
		if event.EventType != turn.EventExternalFactRecorded {
			continue
		}
		var envelope struct {
			ExternalFactKind string `json:"external_fact_kind"`
			ResultContent    string `json:"result_content"`
		}
		if err := json.Unmarshal(event.Payload, &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.ExternalFactKind != "handbook.noop" {
			continue
		}
		factCount++
		if event.ToolCallID != "noop-write" || event.CommandID != "handbook-noop:"+boundTurn+":noop-write" {
			t.Fatalf("unexpected noop event identity: tool_call_id=%q command_id=%q", event.ToolCallID, event.CommandID)
		}
		var fact handbookNoopFact
		if err := json.Unmarshal([]byte(envelope.ResultContent), &fact); err != nil {
			t.Fatal(err)
		}
		if fact.Version != 1 || fact.ReceiptID != "receipt-noop" || fact.SessionID != boundSession || fact.TurnID != boundTurn || fact.ToolCallID != "noop-write" || fact.ToolName != "write_file" || fact.Path != "guide.md" || fact.Digest != handbookfs.Digest([]byte("stable")) {
			t.Fatalf("noop fact=%+v", fact)
		}
		found = true
	}
	if !found || factCount != 1 || client.calls != 2 {
		t.Fatalf("noop fact=%v count=%d model calls=%d", found, factCount, client.calls)
	}
}

func TestHandbookMaintenanceNoopAuditFailureCannotSucceed(t *testing.T) {
	workspace, handbook := t.TempDir(), t.TempDir()
	reg, _ := tools.NewRegistry(workspace, 30)
	_ = reg.SetBuiltinEnabled(config.ExpandBuiltinToolGroups([]string{"filesystem"}))
	_ = reg.SetHandbookRoot(handbook)
	if err := os.WriteFile(filepath.Join(handbook, "guide.md"), []byte("stable"), 0644); err != nil {
		t.Fatal(err)
	}
	turnStore, err := store.Open(filepath.Join(workspace, "turn-events.db"))
	if err != nil {
		t.Fatal(err)
	}
	client := &handbookNoopClient{store: turnStore, closeStore: true}
	allow := policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"write_file": policy.ModeNever}})
	mgr := NewManager("agent-1", stream.NewHub(16, logx.Discard()), client, reg, allow, turnStore, TurnOptions{AutoAgent: true}, logx.Discard())
	defer mgr.Stop()
	rt, _, err := mgr.CreateWithOptionsAndLLM("maintenance-noop-failure", TurnOptions{AutoAgent: true}, reg, nil, client, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	leaseCtx, release, acquired, err := mgr.TryAcquireMaintenanceContext(context.Background(), "agent-1")
	if err != nil || !acquired {
		t.Fatalf("gate: %v %v", acquired, err)
	}
	defer release()
	leaseCtx = handbookfs.WithProvenance(leaseCtx, handbookfs.Provenance{MaintenanceReceiptID: "receipt-noop-failure", SessionID: rt.ID})
	result, err := mgr.RunHandbookMaintenanceWithBinding(leaseCtx, rt.ID, "整理手册", turn.TurnBudget{MaxSteps: 2, MaxTotalTokens: 100}, func(_, _ string) error { return nil })
	if err == nil || result.Changed || client.calls != 1 {
		t.Fatalf("audit failure result=%+v err=%v calls=%d", result, err, client.calls)
	}
}

func TestHandbookMaintenanceReadOnlyRoundReportsNoChange(t *testing.T) {
	workspace, handbook := t.TempDir(), t.TempDir()
	reg, _ := tools.NewRegistry(workspace, 30)
	_ = reg.SetBuiltinEnabled(config.ExpandBuiltinToolGroups([]string{"filesystem"}))
	if err := reg.SetHandbookRoot(handbook); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(handbook, "guide.md"), []byte("stable"), 0644); err != nil {
		t.Fatal(err)
	}
	client := &handbookRoundClient{}
	turnStore, err := store.Open(filepath.Join(workspace, "turn-events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer turnStore.Close()
	allow := policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"read_file": policy.ModeNever, "write_file": policy.ModeNever, "search_replace": policy.ModeNever, "glob_files": policy.ModeNever, "grep_file": policy.ModeNever, "grep_files": policy.ModeNever}})
	mgr := NewManager("agent-1", stream.NewHub(16, logx.Discard()), client, reg, allow, turnStore, TurnOptions{AutoAgent: true}, logx.Discard())
	defer mgr.Stop()
	rt, _, err := mgr.CreateWithOptionsAndLLM("maintenance", TurnOptions{AutoAgent: true}, reg, nil, client, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	leaseCtx, release, ok, err := mgr.TryAcquireMaintenanceContext(context.Background(), "agent-1")
	if err != nil || !ok {
		t.Fatalf("gate: %v %v", ok, err)
	}
	defer release()
	var boundSession, boundTurn string
	result, err := mgr.RunHandbookMaintenanceWithBinding(leaseCtx, rt.ID, "整理经验手册", turn.TurnBudget{MaxSteps: 2, MaxTotalTokens: 100}, func(sessionID, turnID string) error {
		boundSession, boundTurn = sessionID, turnID
		events, eventErr := turnStore.ListTurnEventsForTurn(context.Background(), sessionID, turnID)
		started := false
		for _, event := range events {
			started = started || event.EventType == turn.EventTurnStarted
		}
		if eventErr != nil || !started {
			return fmt.Errorf("turn.started was not durable: events=%+v err=%v", events, eventErr)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed {
		t.Fatal("read-only round reported changed")
	}
	if result.Usage.ToolCalls != 1 {
		t.Fatalf("tool calls=%d", result.Usage.ToolCalls)
	}
	if boundSession != rt.ID || boundTurn == "" {
		t.Fatalf("binding=%q/%q", boundSession, boundTurn)
	}
	beforeCalls := client.calls
	failed, err := mgr.RunHandbookMaintenanceWithBinding(leaseCtx, rt.ID, "绑定失败", turn.TurnBudget{MaxSteps: 1, MaxTotalTokens: 20}, func(string, string) error { return fmt.Errorf("bind rejected") })
	if err == nil || failed.Unknown || !failed.UsageKnown || client.calls != beforeCalls {
		t.Fatalf("binding failure result=%+v err=%v calls=%d", failed, err, client.calls)
	}
	if _, active, state, stateErr := mgr.RuntimeInfo(rt.ID); stateErr != nil || active {
		t.Fatalf("binding failure left active turn: active=%v state=%+v err=%v", active, state, stateErr)
	}
}

func TestHandbookMaintenanceEditRoundAndExecutionBoundary(t *testing.T) {
	workspace, handbook := t.TempDir(), t.TempDir()
	reg, _ := tools.NewRegistry(workspace, 30)
	_ = reg.SetBuiltinEnabled(config.ExpandBuiltinToolGroups([]string{"filesystem"}))
	if err := reg.SetHandbookRoot(handbook); err != nil {
		t.Fatal(err)
	}
	client := &handbookEditClient{}
	allow := policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"read_file": policy.ModeNever, "write_file": policy.ModeNever, "search_replace": policy.ModeNever, "glob_files": policy.ModeNever, "grep_file": policy.ModeNever, "grep_files": policy.ModeNever}})
	mgr := NewManager("agent-1", stream.NewHub(16, logx.Discard()), client, reg, allow, nil, TurnOptions{AutoAgent: true}, logx.Discard())
	defer mgr.Stop()
	rt, _, err := mgr.CreateWithOptionsAndLLM("maintenance", TurnOptions{AutoAgent: true}, reg, nil, client, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	leaseCtx, release, ok, err := mgr.TryAcquireMaintenanceContext(context.Background(), "agent-1")
	if err != nil || !ok {
		t.Fatalf("gate: %v %v", ok, err)
	}
	defer release()
	result, err := mgr.RunHandbookMaintenance(leaseCtx, rt.ID, "整理手册", turn.TurnBudget{MaxSteps: 5, MaxTotalTokens: 200})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Usage.ToolCalls != 3 {
		t.Fatalf("result=%+v", result)
	}
	if raw, err := os.ReadFile(filepath.Join(handbook, "a", "b", "c.md")); err != nil || string(raw) != "new" {
		t.Fatalf("file=%q err=%v", raw, err)
	}
	if _, err := reg.Execute(tools.WithHandbookMaintenance(context.Background()), "bash_run", `{"command":"echo escape"}`); err == nil {
		t.Fatal("expected exec rejection")
	}
	if _, err := reg.Execute(tools.WithHandbookMaintenance(context.Background()), "read_file", `{"path":"hello.txt"}`); err == nil {
		t.Fatal("expected workspace path rejection")
	}
}

func TestHandbookMaintenanceUsageUnknownWhenIntermediateModelOmitsUsage(t *testing.T) {
	workspace, handbook := t.TempDir(), t.TempDir()
	reg, _ := tools.NewRegistry(workspace, 30)
	_ = reg.SetBuiltinEnabled(config.ExpandBuiltinToolGroups([]string{"filesystem"}))
	_ = reg.SetHandbookRoot(handbook)
	client := &handbookEditClient{noUsageSecond: true}
	allow := policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"read_file": policy.ModeNever, "write_file": policy.ModeNever, "search_replace": policy.ModeNever, "glob_files": policy.ModeNever, "grep_file": policy.ModeNever, "grep_files": policy.ModeNever}})
	mgr := NewManager("agent-1", stream.NewHub(16, logx.Discard()), client, reg, allow, nil, TurnOptions{AutoAgent: true}, logx.Discard())
	defer mgr.Stop()
	rt, _, err := mgr.CreateWithOptionsAndLLM("maintenance", TurnOptions{AutoAgent: true}, reg, nil, client, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	leaseCtx, release, ok, err := mgr.TryAcquireMaintenanceContext(context.Background(), "agent-1")
	if err != nil || !ok {
		t.Fatalf("gate: %v %v", ok, err)
	}
	defer release()
	result, err := mgr.RunHandbookMaintenance(leaseCtx, rt.ID, "整理手册", turn.TurnBudget{MaxSteps: 5, MaxTotalTokens: 200})
	if err != nil {
		t.Fatal(err)
	}
	if result.UsageKnown || !result.Unknown {
		t.Fatalf("expected unknown usage: %+v", result)
	}
}

func TestHandbookMaintenanceAskCannotReportSuccess(t *testing.T) {
	workspace, handbook := t.TempDir(), t.TempDir()
	reg, _ := tools.NewRegistry(workspace, 30)
	_ = reg.SetBuiltinEnabled(config.ExpandBuiltinToolGroups([]string{"filesystem"}))
	_ = reg.SetHandbookRoot(handbook)
	client := &handbookAskClient{}
	allow := policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"ask_user_information": policy.ModeNever}})
	mgr := NewManager("agent-1", stream.NewHub(8, logx.Discard()), client, reg, allow, nil, TurnOptions{AutoAgent: true}, logx.Discard())
	defer mgr.Stop()
	rt, _, err := mgr.CreateWithOptionsAndLLM("maintenance", TurnOptions{AutoAgent: true}, reg, nil, client, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	leaseCtx, release, ok, err := mgr.TryAcquireMaintenanceContext(context.Background(), "agent-1")
	if err != nil || !ok {
		t.Fatalf("gate: %v %v", ok, err)
	}
	defer release()
	if _, err := mgr.RunHandbookMaintenance(leaseCtx, rt.ID, "ask", turn.TurnBudget{MaxSteps: 2, MaxTotalTokens: 20}); err == nil {
		t.Fatal("ask_user_information reported success")
	}
}

func TestHandbookMaintenanceLeaseCancelStopsBlockedModelAndCanReacquire(t *testing.T) {
	workspace, handbook := t.TempDir(), t.TempDir()
	reg, _ := tools.NewRegistry(workspace, 30)
	_ = reg.SetBuiltinEnabled(config.ExpandBuiltinToolGroups([]string{"filesystem"}))
	_ = reg.SetHandbookRoot(handbook)
	client := &handbookBlockingClient{started: make(chan struct{})}
	mgr := NewManager("agent-1", stream.NewHub(8, logx.Discard()), client, reg, policy.NewDefaultEngine(), nil, TurnOptions{AutoAgent: true}, logx.Discard())
	defer mgr.Stop()
	rt, _, err := mgr.CreateWithOptionsAndLLM("maintenance", TurnOptions{AutoAgent: true}, reg, nil, client, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	leaseCtx, release, ok, err := mgr.TryAcquireMaintenanceContext(context.Background(), "agent-1")
	if err != nil || !ok {
		t.Fatalf("gate: %v %v", ok, err)
	}
	done := make(chan error, 1)
	go func() {
		_, runErr := mgr.RunHandbookMaintenance(leaseCtx, rt.ID, "blocked", turn.TurnBudget{MaxSteps: 1, MaxTotalTokens: 20})
		done <- runErr
	}()
	select {
	case <-client.started:
	case <-time.After(time.Second):
		t.Fatal("model did not start")
	}
	release()
	select {
	case runErr := <-done:
		if runErr == nil {
			t.Fatal("cancelled model reported success")
		}
	case <-time.After(time.Second):
		t.Fatal("blocked model did not stop")
	}
	if _, err := os.Stat(filepath.Join(handbook, "written.md")); !os.IsNotExist(err) {
		t.Fatalf("unexpected write: %v", err)
	}
	if _, release2, ok, err := mgr.TryAcquireMaintenanceContext(context.Background(), "agent-1"); err != nil || !ok {
		if release2 != nil {
			release2()
		}
		t.Fatalf("gate not reusable: %v %v", ok, err)
	}
}

func TestHandbookMaintenanceBudgetRejectsWithoutLLM(t *testing.T) {
	workspace, handbook := t.TempDir(), t.TempDir()
	reg, _ := tools.NewRegistry(workspace, 30)
	_ = reg.SetBuiltinEnabled(config.ExpandBuiltinToolGroups([]string{"filesystem"}))
	_ = reg.SetHandbookRoot(handbook)
	client := &handbookRoundClient{}
	mgr := NewManager("agent-1", stream.NewHub(8, logx.Discard()), client, reg, policy.NewDefaultEngine(), nil, TurnOptions{AutoAgent: true}, logx.Discard())
	defer mgr.Stop()
	rt, _, err := mgr.CreateWithOptionsAndLLM("maintenance", TurnOptions{AutoAgent: true}, reg, nil, client, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	leaseCtx, release, ok, _ := mgr.TryAcquireMaintenanceContext(context.Background(), "agent-1")
	if !ok {
		t.Fatal("gate")
	}
	defer release()
	if _, err := mgr.RunHandbookMaintenance(leaseCtx, rt.ID, "x", turn.TurnBudget{}); err == nil {
		t.Fatal("expected budget rejection")
	}
	if client.calls != 0 {
		t.Fatalf("llm calls=%d", client.calls)
	}
}

func TestHandbookMaintenanceRequiresOwningLease(t *testing.T) {
	workspace, handbook := t.TempDir(), t.TempDir()
	reg, _ := tools.NewRegistry(workspace, 30)
	_ = reg.SetBuiltinEnabled(config.ExpandBuiltinToolGroups([]string{"filesystem"}))
	_ = reg.SetHandbookRoot(handbook)
	client := &handbookRoundClient{}
	mgr := NewManager("agent-1", stream.NewHub(8, logx.Discard()), client, reg, policy.NewDefaultEngine(), nil, TurnOptions{AutoAgent: true}, logx.Discard())
	defer mgr.Stop()
	rt, _, err := mgr.CreateWithOptionsAndLLM("maintenance", TurnOptions{AutoAgent: true}, reg, nil, client, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	_, release, ok, err := mgr.TryAcquireMaintenanceContext(context.Background(), "agent-1")
	if err != nil || !ok {
		t.Fatalf("gate: %v %v", ok, err)
	}
	defer release()
	if _, err := mgr.RunHandbookMaintenance(context.Background(), rt.ID, "x", turn.TurnBudget{MaxSteps: 1, MaxTotalTokens: 10}); err == nil {
		t.Fatal("maintenance accepted a lease it does not own")
	}
}
