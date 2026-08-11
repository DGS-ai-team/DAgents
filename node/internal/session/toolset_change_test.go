package session

import (
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func TestNotifyToolsetChanged_publishesNoticeAndInterruptsPending(t *testing.T) {
	mgr := testManager(t)
	defer mgr.Stop()

	sess, _, err := mgr.Create("agt-toolset")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	if rt == nil {
		t.Fatal("runtime missing")
	}

	rt.mu.Lock()
	rt.messages = []llm.Message{{
		Role: "assistant",
		ToolCalls: []llm.ToolCall{{
			ID:   "call-pending-1",
			Type: "function",
			Function: llm.ToolCallFunction{
				Name:      "bash_run",
				Arguments: `{"command":"sleep 99"}`,
			},
		}},
	}}
	rt.pending = &turn.PendingHITL{Items: []turn.PendingHITLItem{{
		ToolCall: llm.ToolCall{
			ID:   "call-pending-1",
			Type: "function",
			Function: llm.ToolCallFunction{Name: "bash_run", Arguments: `{"command":"sleep 99"}`},
		},
	}}}
	rt.mu.Unlock()

	events := mgr.hub.SubscribeAgent(mgr.hub.CurrentSeq(), sess.ID)
	defer mgr.hub.Unsubscribe(events)

	mgr.NotifyToolsetChanged(sess.ID)

	deadline := time.Now().Add(2 * time.Second)
	gotNotice := false
	for time.Now().Before(deadline) && !gotNotice {
		select {
		case ev := <-events:
			if ev.Type == "system_notice" {
				msg, _ := ev.Data["message"].(string)
				if msg != ToolsetChangedNotice {
					t.Fatalf("notice=%q", msg)
				}
				gotNotice = true
			}
		case <-time.After(20 * time.Millisecond):
		}
	}
	if !gotNotice {
		t.Fatal("expected system_notice")
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.pending != nil {
		t.Fatal("pending should be cleared")
	}
	found := false
	for _, m := range rt.messages {
		if m.Role == "tool" && m.ToolCallID == "call-pending-1" {
			found = true
			if m.Content != ToolsetChangedInterruptMessage {
				t.Fatalf("tool content=%q", m.Content)
			}
		}
	}
	if !found {
		t.Fatal("expected interrupted tool result in history")
	}
}
