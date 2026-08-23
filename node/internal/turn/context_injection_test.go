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
