package turn

import (
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

func TestBridgeApplyUserMessage_mergesToolBody(t *testing.T) {
	hub := stream.NewHub(4, logx.Discard())
	orch := testOrchestrator(t, hub, &llm.MockClient{})

	built := orch.BuildAsyncSideEffectMessages("s", nil, queue.AsyncToolResultPayload{
		JobID: "job-1", ToolName: "bash_run", Status: "succeeded", ResultText: "done",
	})

	msg := bridgeApplyUserMessage(built)
	if msg.Role != "user" || msg.Name != llm.UserNameAsyncTool {
		t.Fatalf("msg = %+v", msg)
	}
	if !strings.Contains(msg.Content, "job_id=job-1") {
		t.Fatalf("user missing meta: %q", msg.Content)
	}
	if !strings.Contains(msg.Content, "[ASYNC_TOOL_RESULT]") {
		t.Fatalf("user missing tool body: %q", msg.Content)
	}
}

func TestShouldContinueAfterSideEffectApplyMessages_bridgeUserTail(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "done"},
		{Role: "user", Content: "callback", Name: llm.UserNameAsyncTool},
	}
	if !ShouldContinueAfterSideEffectApply(msgs) {
		t.Fatal("expected bridge user tail to continue")
	}
	if ShouldContinueAfterSideEffectApply([]llm.Message{
		{Role: "user", Content: "hi", Name: llm.UserNameHuman},
	}) {
		t.Fatal("human user tail must not auto-continue")
	}
}
