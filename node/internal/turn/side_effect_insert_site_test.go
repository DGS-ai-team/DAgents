package turn

import (
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

func TestResolveSideEffectInsertSite_table(t *testing.T) {
	hub := stream.NewHub(4, logx.Discard())
	orch := testOrchestrator(t, hub, &llm.MockClient{})
	built := orch.BuildSideEffectMessages(SideEffectAsync, "s", nil, queue.AsyncToolResultPayload{
		JobID: "job-1", ToolName: "bash_run", Status: "succeeded", ResultText: "done",
	}, "", "")

	tests := []struct {
		name     string
		messages []llm.Message
		wantMode string
		wantAt   int
		wantCont bool
	}{
		{
			name:     "empty_history_bridge",
			messages: nil,
			wantMode: "empty_history_bridge",
			wantAt:   0,
			wantCont: true,
		},
		{
			name: "tail_tool_append",
			messages: []llm.Message{
				{Role: "user", Content: "run"},
				{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c1", Function: llm.ToolCallFunction{Name: "bash"}}}},
				{Role: "tool", ToolCallID: "c1", Content: "ok"},
			},
			wantMode: "append_callback",
			wantAt:   3,
			wantCont: true,
		},
		{
			name: "assistant_with_unresponded_tools",
			messages: []llm.Message{
				{Role: "user", Content: "run"},
				{Role: "assistant", ToolCalls: []llm.ToolCall{
					{ID: "bg", Function: llm.ToolCallFunction{Name: "bash_run"}},
					{ID: "sync", Function: llm.ToolCallFunction{Name: "read_file"}},
				}},
			},
			wantMode: "insert_before_last_assistant",
			wantAt:   1,
			wantCont: false,
		},
		{
			name: "open_batch_after_bg_ack",
			messages: []llm.Message{
				{Role: "user", Content: "run"},
				{Role: "assistant", ToolCalls: []llm.ToolCall{
					{ID: "bg", Function: llm.ToolCallFunction{Name: "bash_run"}},
					{ID: "sync", Function: llm.ToolCallFunction{Name: "read_file"}},
				}},
				{Role: "tool", ToolCallID: "bg", Content: "[TOOL_BACKGROUND] job_id=j1"},
			},
			wantMode: "append_callback",
			wantAt:   3,
			wantCont: true,
		},
		{
			name: "bridge_user_callback",
			messages: []llm.Message{
				{Role: "user", Content: "hi"},
				{Role: "assistant", Content: "done"},
			},
			wantMode: "bridge_user_callback",
			wantAt:   2,
			wantCont: true,
		},
		{
			name: "tail_other_fallback",
			messages: []llm.Message{
				{Role: "user", Content: "only user"},
			},
			wantMode: "append_callback_fallback",
			wantAt:   1,
			wantCont: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			site := ResolveSideEffectInsertSite(tc.messages, built)
			if !site.Ready {
				t.Fatalf("site not ready: %+v", site)
			}
			if site.Mode != tc.wantMode {
				t.Fatalf("mode = %q, want %q", site.Mode, tc.wantMode)
			}
			if site.InsertAt != tc.wantAt {
				t.Fatalf("insertAt = %d, want %d", site.InsertAt, tc.wantAt)
			}
			if site.Continue != tc.wantCont {
				t.Fatalf("continue = %v, want %v", site.Continue, tc.wantCont)
			}
			plan := PlanSingleSideEffectApply(tc.messages, built)
			if len(plan.Messages) == 0 {
				t.Fatal("expected non-empty apply plan")
			}
			if tc.wantMode == "bridge_user_callback" && len(plan.Messages) != 3 {
				t.Fatalf("bridge plan len = %d, want 3", len(plan.Messages))
			}
		})
	}
}
