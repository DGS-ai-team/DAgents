package turn

import (
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func TestApplyContextInjectionsAnchorsBeforeCurrentRootUser(t *testing.T) {
	history := []llm.Message{
		llm.UserMessage("旧任务", llm.UserNameHuman),
		{Role: "assistant", Content: "旧结论"},
		llm.UserMessage("当前任务", llm.UserNameHuman),
		{Role: "assistant", Content: "处理中"},
		llm.ToolResultMessage("call-1", "read_file", "结果"),
	}
	injections := []ContextInjection{{Content: "## 当前运行上下文\n事实", Name: "runtime_context", Source: "runtime_context"}}

	request := ApplyContextInjections(history, injections)
	if len(request) != len(history)+1 {
		t.Fatalf("request len = %d, history = %d: %+v", len(request), len(history), request)
	}
	if request[2].Name != llm.UserNameContext || !strings.Contains(request[2].Content, "当前运行上下文") {
		t.Fatalf("injection should precede current root user: %+v", request)
	}
	if request[3].Content != "当前任务" || request[4].Content != "处理中" {
		t.Fatalf("current turn continuity changed: %+v", request)
	}
	for _, message := range history {
		if message.Name == llm.UserNameContext {
			t.Fatal("ApplyContextInjections mutated the source history")
		}
	}
}

func TestApplyContextInjectionsRemovesRequestOnlyDuplicates(t *testing.T) {
	history := []llm.Message{
		{Role: "system", Content: "stable"},
		llm.UserMessage("context-old", llm.UserNameContext),
		llm.UserMessage("task", llm.UserNameHuman),
	}
	request := ApplyContextInjections(history, []ContextInjection{{Content: "context-new"}})
	count := 0
	for _, message := range request {
		if message.Name == llm.UserNameContext {
			count++
			if message.Content != "context-new" {
				t.Fatalf("stale context survived: %+v", request)
			}
		}
	}
	if count != 1 {
		t.Fatalf("context count = %d, request = %+v", count, request)
	}
	if request[1].Role != "user" || request[1].Name != llm.UserNameContext {
		t.Fatalf("context should be after system and before root user: %+v", request)
	}
}

func TestApplyContextInjectionsPlacesMemoryAfterCurrentRootUser(t *testing.T) {
	history := []llm.Message{
		llm.UserMessage("旧任务", llm.UserNameHuman),
		{Role: "assistant", Content: "旧结论"},
		llm.UserMessage("当前任务", llm.UserNameHuman),
		{Role: "assistant", Content: "处理中"},
	}
	injections := []ContextInjection{
		{Name: llm.UserNameContext, Source: "runtime_context", Content: "运行事实", Position: "before_current_user"},
		{Name: llm.UserNameMemoryContext, Source: "memory", Content: "历史偏好", Position: "after_current_user", MessageKind: llm.MessageSourceMemory, MessageForm: llm.MessageFormSnapshot},
	}

	request := ApplyContextInjections(history, injections)
	if len(request) != len(history)+2 {
		t.Fatalf("request len = %d: %+v", len(request), request)
	}
	if request[2].Name != llm.UserNameContext || request[2].Content != "运行事实" {
		t.Fatalf("runtime context placement = %+v", request)
	}
	if request[3].Content != "当前任务" || request[4].Name != llm.UserNameMemoryContext || request[4].Content != "历史偏好" {
		t.Fatalf("memory context must follow current user: %+v", request)
	}
	if request[5].Content != "处理中" {
		t.Fatalf("tool continuation moved around memory context: %+v", request)
	}
	if durable := StripContextInjections(request); len(durable) != len(history) {
		t.Fatalf("request-only memory/runtime context leaked to durable history: %+v", durable)
	}
}

func TestModelContextSnapshotClonesContextInjections(t *testing.T) {
	injections := []ContextInjection{{Name: "runtime_context", Source: "runtime", Content: "cwd=/workspace"}}
	snapshot := NewModelContextSnapshotWithInjections("stable", nil, injections, 7, "runtime")
	if snapshot.ContextInjectionDigest == "" {
		t.Fatal("context injection digest is empty")
	}
	injections[0].Content = "mutated"
	clone := snapshot.Clone()
	clone.ContextInjections[0].Content = "clone-mutated"
	if snapshot.ContextInjections[0].Content != "cwd=/workspace" {
		t.Fatalf("snapshot was mutated through input/clone: %+v", snapshot)
	}
	if clone.ContextInjections[0].Content != "clone-mutated" {
		t.Fatalf("clone did not copy injection: %+v", clone)
	}
}
