package turn

import (
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

func TestPublishToolResultUsesAuthoritativeStatusWithoutDuplicateFields(t *testing.T) {
	hub := stream.NewHub(8, logx.Discard())
	o := NewOrchestrator("agent-1", t.TempDir(), hub, &llm.MockClient{}, nil, nil, SkillAccess{}, nil, nil, hooks.RuntimeConfig{}, logx.Discard())
	ch := hub.Subscribe(hub.CurrentSeq())
	defer hub.Unsubscribe(ch)

	o.publishToolResult("session-1", llm.ToolCall{
		ID: "call-1", Type: "function", Function: llm.ToolCallFunction{Name: "terminal_command"},
	}, "ERROR: connection refused", true, nil)

	select {
	case event := <-ch:
		if event.Type != "tool_result" {
			t.Fatalf("event type=%q", event.Type)
		}
		if event.Data["status"] != "failed" {
			t.Fatalf("status=%v data=%+v", event.Data["status"], event.Data)
		}
		if _, ok := event.Data["rejected"]; ok {
			t.Fatalf("tool result must not expose duplicate rejected field: %+v", event.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for tool_result")
	}

}

func TestPublishToolResultUsesAsyncStatusWhenPreviewIsCleaned(t *testing.T) {
	hub := stream.NewHub(8, logx.Discard())
	o := NewOrchestrator("agent-1", t.TempDir(), hub, &llm.MockClient{}, nil, nil, SkillAccess{}, nil, nil, hooks.RuntimeConfig{}, logx.Discard())
	ch := hub.Subscribe(hub.CurrentSeq())
	defer hub.Unsubscribe(ch)

	o.publishToolResult("session-1", llm.ToolCall{
		ID: "async-1", Type: "function", Function: llm.ToolCallFunction{Name: "tool_callback"},
	}, "cleaned failure preview", false, map[string]any{"async_status": "failed"})

	select {
	case event := <-ch:
		if event.Data["status"] != "failed" {
			t.Fatalf("status=%v data=%+v", event.Data["status"], event.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for async tool_result")
	}
}
