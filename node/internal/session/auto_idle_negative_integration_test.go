package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

type negativeAutoIdleLLM struct {
	mu    sync.Mutex
	mode  string
	calls int
}

func (c *negativeAutoIdleLLM) StreamChat(_ context.Context, _ llm.ChatRequest, _ llm.StreamHandler) (llm.ChatResult, error) {
	c.mu.Lock()
	c.calls++
	call := c.calls
	mode := c.mode
	c.mu.Unlock()
	switch mode {
	case "spoof":
		return llm.ChatResult{ToolCalls: []llm.ToolCall{{ID: "spoof-idle", Type: "function", Function: llm.ToolCallFunction{Name: "auto_idle", Arguments: `{}`}}}, FinishReason: "tool_calls"}, nil
	case "write":
		if call == 1 {
			return llm.ChatResult{ToolCalls: []llm.ToolCall{{ID: "write-before-idle", Type: "function", Function: llm.ToolCallFunction{Name: "write_file", Arguments: `{"path":"written.txt","content":"changed","call_purpose":"test"}`}}}, FinishReason: "tool_calls"}, nil
		}
		return llm.ChatResult{ToolCalls: []llm.ToolCall{{ID: "idle-after-write", Type: "function", Function: llm.ToolCallFunction{Name: "auto_idle", Arguments: `{}`}}}, FinishReason: "tool_calls"}, nil
	case "ask":
		if call == 1 {
			return llm.ChatResult{ToolCalls: []llm.ToolCall{{ID: "ask-before-idle", Type: "function", Function: llm.ToolCallFunction{Name: "ask_user_information", Arguments: `{"question":"confirm"}`}}}, FinishReason: "tool_calls"}, nil
		}
		return llm.ChatResult{ToolCalls: []llm.ToolCall{{ID: "idle-after-ask", Type: "function", Function: llm.ToolCallFunction{Name: "auto_idle", Arguments: `{}`}}}, FinishReason: "tool_calls"}, nil
	default:
		return llm.ChatResult{Content: "done", FinishReason: "stop"}, nil
	}
}
func (*negativeAutoIdleLLM) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}
func (*negativeAutoIdleLLM) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func negativeAutoIdleFixture(t *testing.T, mode string) (*Manager, *negativeAutoIdleLLM, *stream.Hub, string, string, func()) {
	t.Helper()
	workspace := t.TempDir()
	reg, err := tools.NewRegistry(workspace, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.SetBuiltinEnabled(config.ExpandBuiltinToolGroups([]string{"filesystem"})); err != nil {
		t.Fatal(err)
	}
	client := &negativeAutoIdleLLM{mode: mode}
	hub := stream.NewHub(32, logx.Discard())
	pol := policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{
		"write_file": policy.ModeNever, "auto_idle": policy.ModeNever,
	}})
	mgr := NewManager("agent-negative", hub, client, reg, pol, nil, TurnOptions{
		AutoAgent: true, WorkspaceRoot: workspace,
		TriggerToolRoundProvider: func(_ context.Context, agentID, triggerID, deliveryID string) (int, bool, error) {
			return 3, agentID == "agent-negative" && triggerID == "auto-idle-test" && deliveryID == "delivery-1", nil
		},
	}, logx.Discard())
	hub.SetEventListener(mgr.OnStreamEvent)
	// Production Auto uses the canonical Agent ID as its main session ID; this
	// also lets the real Hub listener update that session's notification state.
	sess, _, err := mgr.Create("agent-negative")
	if err != nil {
		mgr.Stop()
		t.Fatal(err)
	}
	return mgr, client, hub, sess.ID, workspace, func() { mgr.Stop() }
}

func waitTurnFinished(t *testing.T, events <-chan stream.Event) stream.Event {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.Type == "turn_finished" {
				return ev
			}
		case <-deadline:
			t.Fatal("timed out waiting for turn_finished")
		}
	}
}

func TestAutoIdleNegativeCasesDoNotSuppressNotifications(t *testing.T) {
	t.Run("ordinary chat spoof", func(t *testing.T) {
		mgr, client, hub, sid, _, cleanup := negativeAutoIdleFixture(t, "spoof")
		defer cleanup()
		events := hub.Subscribe(0)
		defer hub.Unsubscribe(events)
		if _, err := mgr.EnqueueMessage(context.Background(), sid, "message", "没有工作", nil, nil, ""); err != nil {
			t.Fatal(err)
		}
		ev := waitTurnFinished(t, events)
		if noWork, _ := ev.Data["no_work"].(bool); noWork {
			t.Fatalf("ordinary spoof became no_work: %#v", ev.Data)
		}
		if finish, _ := ev.Data["finish_reason"].(string); finish != "error" {
			t.Fatalf("ordinary auto_idle spoof was not rejected: finish=%q event=%+v", finish, ev)
		}
		if client.calls != 1 {
			t.Fatalf("calls=%d", client.calls)
		}
	})

	t.Run("write before idle", func(t *testing.T) {
		mgr, client, hub, sid, workspace, cleanup := negativeAutoIdleFixture(t, "write")
		defer cleanup()
		events := hub.Subscribe(0)
		defer hub.Unsubscribe(events)
		if err := mgr.EnqueueAutoTriggerMessage(sid, "auto-idle-test", "检查", "delivery-1"); err != nil {
			t.Fatal(err)
		}
		ev := waitTurnFinished(t, events)
		if noWork, _ := ev.Data["no_work"].(bool); noWork {
			t.Fatalf("write then idle became no_work: %#v", ev.Data)
		}
		if finish, _ := ev.Data["finish_reason"].(string); finish != "error" {
			t.Fatalf("write then idle was not rejected: finish=%q event=%+v", finish, ev)
		}
		if _, err := os.Stat(filepath.Join(workspace, "written.txt")); err != nil {
			t.Fatalf("write did not execute: %v", err)
		}
		if client.calls != 2 {
			t.Fatalf("calls=%d", client.calls)
		}
	})

	t.Run("ask recovery then idle", func(t *testing.T) {
		mgr, client, hub, sid, _, cleanup := negativeAutoIdleFixture(t, "ask")
		defer cleanup()
		events := hub.Subscribe(0)
		defer hub.Unsubscribe(events)
		before := mgr.NotificationState(sid)
		if err := mgr.EnqueueAutoTriggerMessage(sid, "auto-idle-test", "检查", "delivery-1"); err != nil {
			t.Fatal(err)
		}
		deadline := time.After(5 * time.Second)
		for {
			select {
			case ev := <-events:
				if ev.Type != "hitl_required" {
					continue
				}
				if _, err := mgr.EnqueueMessage(context.Background(), sid, "resume", "", nil, map[string]any{"type": "user_information", "tool_call_id": "ask-before-idle", "answer": "确认"}, ""); err != nil {
					t.Fatal(err)
				}
				finished := waitTurnFinished(t, events)
				if noWork, _ := finished.Data["no_work"].(bool); noWork {
					t.Fatalf("ASK recovery then idle became no_work: %#v", finished.Data)
				}
				if got := mgr.NotificationState(sid).NotifySeq; got <= before.NotifySeq {
					t.Fatalf("ASK approval did not notify: before=%d after=%d", before.NotifySeq, got)
				}
				if client.calls != 2 {
					t.Fatalf("calls=%d", client.calls)
				}
				return
			case <-deadline:
				t.Fatal("timed out waiting for ASK")
			}
		}
	})
}

type autoIdlePositiveLLM struct {
	mu    sync.Mutex
	calls int
}

func (c *autoIdlePositiveLLM) StreamChat(context.Context, llm.ChatRequest, llm.StreamHandler) (llm.ChatResult, error) {
	c.mu.Lock()
	c.calls++
	call := c.calls
	c.mu.Unlock()
	if call == 1 {
		return llm.ChatResult{Content: "普通完成", FinishReason: "stop"}, nil
	}
	return llm.ChatResult{ToolCalls: []llm.ToolCall{{ID: "positive-idle", Type: "function", Function: llm.ToolCallFunction{Name: "auto_idle", Arguments: `{}`}}}, FinishReason: "tool_calls"}, nil
}
func (*autoIdlePositiveLLM) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}
func (*autoIdlePositiveLLM) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func TestTrustedAutoIdleSuppressesNotifyButPersistsHistory(t *testing.T) {
	workspace := t.TempDir()
	reg, err := tools.NewRegistry(workspace, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.SetBuiltinEnabled(config.ExpandBuiltinToolGroups([]string{"filesystem"})); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(t.TempDir(), "session.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	client := &autoIdlePositiveLLM{}
	hub := stream.NewHub(32, logx.Discard())
	pol := policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"auto_idle": policy.ModeNever}})
	mgr := NewManager("agent-positive", hub, client, reg, pol, st, TurnOptions{AutoAgent: true, WorkspaceRoot: workspace,
		TriggerToolRoundProvider: func(_ context.Context, agentID, triggerID, deliveryID string) (int, bool, error) {
			return 3, agentID == "agent-positive" && triggerID == "auto-default:agent-positive" && deliveryID == "delivery-1", nil
		},
	}, logx.Discard())
	t.Cleanup(func() {
		mgr.Stop()
		_ = st.Close()
	})
	hub.SetEventListener(mgr.OnStreamEvent)
	sess, _, err := mgr.Create("agent-positive")
	if err != nil {
		t.Fatal(err)
	}
	events := hub.Subscribe(0)
	defer hub.Unsubscribe(events)
	if _, err := mgr.EnqueueMessage(context.Background(), sess.ID, "message", "没有工作", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	ordinary := waitTurnFinished(t, events)
	if finish, _ := ordinary.Data["finish_reason"].(string); finish != "stop" {
		t.Fatalf("ordinary finish=%q", finish)
	}
	baseline := mgr.NotificationState(sess.ID).NotifySeq
	if baseline <= 0 {
		t.Fatalf("ordinary natural-language completion did not advance notification: %+v", mgr.NotificationState(sess.ID))
	}
	if err := mgr.EnqueueAutoTriggerMessage(sess.ID, "auto-default:agent-positive", "检查", "delivery-1"); err != nil {
		t.Fatal(err)
	}
	idle := waitTurnFinished(t, events)
	if noWork, _ := idle.Data["no_work"].(bool); !noWork {
		t.Fatalf("trusted idle missing no_work: %#v", idle.Data)
	}
	if got := mgr.NotificationState(sess.ID).NotifySeq; got != baseline {
		t.Fatalf("trusted idle bumped notification: baseline=%d after=%d", baseline, got)
	}
	view, err := mgr.GetHydrateView(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, entry := range view.Transcript {
		joined += " " + strings.TrimSpace(entryText(entry))
	}
	if !strings.Contains(joined, "没有工作") || !strings.Contains(joined, "普通完成") {
		t.Fatalf("history missing before shutdown: %q", joined)
	}
	assertAutoIdleRuntimeHistory(t, mgr.getRuntime(sess.ID).activeMessagesSnapshot())
	assertAutoIdleHistory(t, st, sess.ID)
	mgr.Stop()
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	mgr2 := NewManager("agent-positive", stream.NewHub(16, logx.Discard()), &autoIdlePositiveLLM{}, reg, pol, reopened, TurnOptions{AutoAgent: true, WorkspaceRoot: workspace}, logx.Discard())
	defer mgr2.Stop()
	if _, _, err := mgr2.Create("agent-positive"); err != nil {
		t.Fatal(err)
	}
	view2, err := mgr2.GetHydrateView(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	joined = ""
	for _, entry := range view2.Transcript {
		joined += " " + strings.TrimSpace(entryText(entry))
	}
	if !strings.Contains(joined, "没有工作") || !strings.Contains(joined, "普通完成") {
		t.Fatalf("history missing after reopen: %q", joined)
	}
	assertAutoIdleHistory(t, reopened, sess.ID)
}

func assertAutoIdleHistory(t *testing.T, st *store.SQLiteStore, sessionID string) {
	t.Helper()
	events, err := st.ListTurnEvents(context.Background(), sessionID, 0, 1000)
	if err != nil {
		t.Fatalf("load persisted idle lifecycle: %v", err)
	}
	var foundInput, foundCall, foundResult bool
	for _, event := range events {
		payload := string(event.Payload)
		if event.EventType == turn.EventTurnStarted && strings.Contains(payload, "检查") {
			foundInput = true
		}
		if event.EventType == turn.EventAssistantMessageRecorded && strings.Contains(payload, "positive-idle") && strings.Contains(payload, "auto_idle") {
			foundCall = true
		}
		if (event.EventType == turn.EventToolResultRecorded || event.EventType == turn.EventToolExecutionCompleted) && event.ToolCallID == "positive-idle" {
			var result struct {
				ResultContent string `json:"result_content"`
			}
			if json.Unmarshal(event.Payload, &result) == nil && strings.Contains(strings.ReplaceAll(result.ResultContent, " ", ""), `"no_work":true`) {
				foundResult = true
			}
		}
	}
	if !foundInput || !foundCall || !foundResult {
		t.Fatalf("persisted auto_idle history incomplete: input=%v call=%v result=%v event_count=%d", foundInput, foundCall, foundResult, len(events))
	}
}

func assertAutoIdleRuntimeHistory(t *testing.T, messages []llm.Message) {
	t.Helper()
	var foundInput, foundCall, foundResult bool
	for _, msg := range messages {
		if msg.Role == "user" && strings.Contains(llm.MessageTextSummary(msg), "检查") {
			foundInput = true
		}
		if msg.Role == "assistant" {
			for _, call := range msg.ToolCalls {
				if call.ID == "positive-idle" && call.Function.Name == "auto_idle" {
					foundCall = true
				}
			}
		}
		if msg.Role == "tool" && msg.ToolCallID == "positive-idle" && strings.Contains(strings.ReplaceAll(msg.Content, " ", ""), `"no_work":true`) {
			foundResult = true
		}
	}
	if !foundInput || !foundCall || !foundResult {
		t.Fatalf("active auto_idle history incomplete: input=%v call=%v result=%v messages=%+v", foundInput, foundCall, foundResult, messages)
	}
}

func entryText(entry map[string]any) string {
	if text, ok := entry["text"].(string); ok {
		return text
	}
	return ""
}
