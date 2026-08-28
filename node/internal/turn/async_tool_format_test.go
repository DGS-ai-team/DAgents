package turn

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

func TestFormatAsyncToolUserMessage_includesJobID(t *testing.T) {
	got := formatAsyncToolUserMessage("bash_run", "job-abc", "succeeded", "运行脚本")
	if strings.Contains(got, "job_id已完成") {
		t.Fatalf("user message still has literal job_id placeholder: %q", got)
	}
	if !strings.Contains(got, "job_id=job-abc") {
		t.Fatalf("user message missing job_id value: %q", got)
	}
	if !strings.Contains(got, "目的=运行脚本") {
		t.Fatalf("user message missing purpose: %q", got)
	}
}

func TestLookupAsyncSourceFromHistory_bySourceToolCall(t *testing.T) {
	history := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{
			ID: "call-bg-1", Type: "function",
			Function: llm.ToolCallFunction{
				Name:      "bash_run",
				Arguments: `{"call_purpose":"运行 hello 脚本","command":"./sleep_and_hello.sh"}`,
			},
		}}},
		{Role: "tool", ToolCallID: "call-bg-1", Content: "[BASH_RESULT] status=RUNNING job_id=job-1"},
	}
	src := lookupAsyncSourceFromHistory(history, "bash_run", "job-1", "call-bg-1")
	if src.OriginalToolCallID != "call-bg-1" {
		t.Fatalf("source call id = %q", src.OriginalToolCallID)
	}
	if src.CallPurpose != "运行 hello 脚本" {
		t.Fatalf("purpose = %q", src.CallPurpose)
	}
	if !strings.Contains(src.ParamsSummary, "sleep_and_hello") {
		t.Fatalf("params = %q", src.ParamsSummary)
	}
}

func TestBuildAsyncToolMessages_modelFriendly(t *testing.T) {
	hub := stream.NewHub(4, logx.Discard())
	orch := testOrchestrator(t, hub, &llm.MockClient{})

	history := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{
			ID: "call-bg-1", Type: "function",
			Function: llm.ToolCallFunction{
				Name:      "bash_run",
				Arguments: `{"call_purpose":"测试","command":"echo hi"}`,
			},
		}}},
		{Role: "tool", ToolCallID: "call-bg-1", Content: "[BASH_RESULT] status=RUNNING job_id=job-1"},
	}
	built := orch.BuildAsyncSideEffectMessages("sess-1", history, queue.AsyncToolResultPayload{
		JobID: "job-1", ToolName: "bash_run", ToolCallID: "call-bg-1", Status: "succeeded", ResultText: "done",
	})

	if !strings.Contains(built.UserMessage.Content, "job_id=job-1") {
		t.Fatalf("user = %q", built.UserMessage.Content)
	}
	if built.ToolCallID != "async-job-job-1" {
		t.Fatalf("callback tool_call_id = %q", built.ToolCallID)
	}
	if !strings.Contains(built.ToolMessage.Content, "call_purpose=测试") {
		t.Fatalf("tool = %q", built.ToolMessage.Content)
	}
	if !strings.Contains(built.ToolMessage.Content, "command=echo hi") {
		t.Fatalf("tool missing command: %q", built.ToolMessage.Content)
	}
	if !strings.Contains(built.ToolMessage.Content, "source_tool_call_id=call-bg-1") {
		t.Fatalf("tool missing source call id: %q", built.ToolMessage.Content)
	}
	if built.ToolMessage.ToolResultMetadata == nil || built.ToolMessage.ToolResultMetadata.Status != "succeeded" {
		t.Fatalf("async tool metadata = %+v", built.ToolMessage.ToolResultMetadata)
	}

	var args map[string]any
	if err := json.Unmarshal([]byte(built.AssistantMessage.ToolCalls[0].Function.Arguments), &args); err != nil {
		t.Fatal(err)
	}
	if args["call_purpose"] != "测试" {
		t.Fatalf("callback args = %+v", args)
	}
	if args["source_tool_call_id"] != "call-bg-1" {
		t.Fatalf("callback args = %+v", args)
	}
}

func TestBuildAsyncToolMessages_modelMetadataPreservesTimeoutStatus(t *testing.T) {
	hub := stream.NewHub(4, logx.Discard())
	orch := testOrchestrator(t, hub, &llm.MockClient{})
	built := orch.BuildAsyncSideEffectMessages("sess-1", nil, queue.AsyncToolResultPayload{
		JobID: "job-timeout", ToolName: "bash_run", Status: "timed_out", ErrorText: "命令超时",
	})
	if built.ToolMessage.ToolResultMetadata == nil || built.ToolMessage.ToolResultMetadata.Status != "timed_out" {
		t.Fatalf("timeout metadata = %+v content=%q", built.ToolMessage.ToolResultMetadata, built.ToolMessage.Content)
	}
	if built.ToolMessage.ToolResultMetadata.Error == nil || built.ToolMessage.ToolResultMetadata.Error.Code != "tool_timeout" {
		t.Fatalf("timeout error metadata = %+v", built.ToolMessage.ToolResultMetadata.Error)
	}
}
