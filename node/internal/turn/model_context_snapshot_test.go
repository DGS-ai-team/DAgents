package turn

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/memory"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

func TestModelContextSnapshotCopiesDefinitionsAndDigestsStableMaps(t *testing.T) {
	defs := []tools.ToolDef{{
		Type: "function",
		Function: tools.FunctionDef{
			Name: "example",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"b": "two", "a": "one"},
			},
		},
	}}
	snapshot := NewModelContextSnapshot("system", defs, 7, "")
	defs[0].Function.Parameters["changed"] = true
	if _, ok := snapshot.ToolDefinitions[0].Function.Parameters["changed"]; ok {
		t.Fatal("snapshot tool definitions must be immutable from caller mutations")
	}
	if snapshot.PromptDigest == "" || snapshot.ToolDigest == "" || snapshot.RuntimeDigest == "" {
		t.Fatalf("snapshot digests should be populated: %+v", snapshot)
	}
	left := Digest(map[string]any{"a": 1, "b": 2})
	right := Digest(map[string]any{"b": 2, "a": 1})
	if left != right {
		t.Fatalf("map key order changed digest: %q != %q", left, right)
	}
}

func TestTurnKeepsModelContextSnapshotAcrossToolSteps(t *testing.T) {
	hub := stream.NewHub(32, logx.Discard())
	reg := testRegistry(t)
	if _, err := reg.Execute(context.Background(), "write_file", `{"path":"snapshot.txt","content":"ok"}`); err != nil {
		t.Fatal(err)
	}
	client := &snapshotLLM{}
	pol := policy.NewDefaultEngine()
	orch := NewOrchestrator("a1", t.TempDir(), hub, client, reg, pol, SkillAccess{}, nil, nil,
		hooks.RuntimeConfig{Duplicate: hooks.DefaultDuplicateConfig(), ToolResult: hooks.DefaultToolResultConfig(t.TempDir())}, logx.Discard())
	var promptCalls int
	orch.SetSystemPromptBuilder(func(SystemPromptInput) string {
		promptCalls++
		return "prompt-version-" + string(rune('0'+promptCalls))
	})
	orch.SetContextInjectionBuilder(func(SystemPromptInput) []ContextInjection {
		return []ContextInjection{{
			Name:     "runtime_context",
			Source:   "test",
			Content:  "## 测试运行时上下文\ncontext-v1",
			Position: "before_current_user",
		}}
	})

	var history []llm.Message
	_, _, err := runMessageTurnInline(t, orch, context.Background(), "session-1", &history, "读取", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 2 {
		t.Fatalf("expected two model steps, got %d", len(client.requests))
	}
	if client.requests[0].SystemPrompt != client.requests[1].SystemPrompt {
		t.Fatalf("system prompt changed within Turn: %q != %q", client.requests[0].SystemPrompt, client.requests[1].SystemPrompt)
	}
	if len(client.requests[0].Tools) != len(client.requests[1].Tools) {
		t.Fatalf("tool schema count changed within Turn: %d != %d", len(client.requests[0].Tools), len(client.requests[1].Tools))
	}
	if len(client.requests[1].Messages) == 0 || !strings.Contains(client.requests[1].Messages[len(client.requests[1].Messages)-1].Content, "[TOOL_RESULT_METADATA]") {
		t.Fatalf("model request did not receive tool result metadata: %+v", client.requests[1].Messages)
	}
	for index, request := range client.requests {
		contextCount := 0
		contextIndex := -1
		for i, message := range request.Messages {
			if message.Name == llm.UserNameContext {
				contextCount++
				contextIndex = i
			}
		}
		if contextCount != 1 {
			t.Fatalf("request %d context count = %d: %+v", index, contextCount, request.Messages)
		}
		if contextIndex < 0 || contextIndex+1 >= len(request.Messages) || request.Messages[contextIndex+1].Content != "读取" {
			t.Fatalf("request %d context is not anchored before current user: %+v", index, request.Messages)
		}
	}
	if len(history) == 0 || strings.Contains(history[len(history)-1].Content, "[TOOL_RESULT_METADATA]") {
		t.Fatalf("persisted history should keep raw tool result: %+v", history)
	}
	for _, message := range history {
		if message.Name == llm.UserNameContext {
			t.Fatalf("request-only context leaked into durable history: %+v", history)
		}
	}
	if promptCalls == 0 {
		t.Fatal("system prompt builder was not called")
	}
	if orch.ModelContextSnapshot("session-1") != nil {
		t.Fatal("completed Turn must release its model context snapshot")
	}
}

func TestTurnFreezesRequestOnlyMemoryContextAcrossToolSteps(t *testing.T) {
	hub := stream.NewHub(32, logx.Discard())
	service, err := memory.OpenLocalService(t.TempDir()+"/agent.db", t.TempDir()+"/global.db", memory.ScopeAgent)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if _, err := service.Remember(context.Background(), memory.RememberRequest{
		Information: "用户偏好中文回复", Tier: memory.TierRecall, Kind: memory.KindPreference,
		SemanticKey: "user.language", Cardinality: "single",
	}); err != nil {
		t.Fatal(err)
	}

	client := &snapshotLLM{}
	pol := policy.NewDefaultEngine()
	orch := NewOrchestrator("a1", t.TempDir(), hub, client, testRegistry(t), pol, SkillAccess{}, nil, nil,
		hooks.RuntimeConfig{Duplicate: hooks.DefaultDuplicateConfig(), ToolResult: hooks.DefaultToolResultConfig(t.TempDir())}, logx.Discard())
	orch.SetMemoryService(service)

	var history []llm.Message
	if _, _, err := runMessageTurnInline(t, orch, context.Background(), "session-memory", &history, "读取我的语言偏好", nil); err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 2 {
		t.Fatalf("expected two model steps, got %d", len(client.requests))
	}
	var firstMemory, secondMemory string
	for index, request := range client.requests {
		memoryIndex := -1
		userIndex := -1
		for i, message := range request.Messages {
			if message.Name == llm.UserNameMemoryContext {
				if memoryIndex >= 0 {
					t.Fatalf("request %d contains duplicate memory context: %+v", index, request.Messages)
				}
				memoryIndex = i
			}
			if message.Content == "读取我的语言偏好" && message.Role == "user" {
				userIndex = i
			}
		}
		if memoryIndex < 0 || userIndex < 0 || memoryIndex != userIndex+1 {
			t.Fatalf("request %d memory context must follow current user: %+v", index, request.Messages)
		}
		if index == 0 {
			firstMemory = request.Messages[memoryIndex].Content
		} else {
			secondMemory = request.Messages[memoryIndex].Content
		}
	}
	if firstMemory == "" || firstMemory != secondMemory {
		t.Fatalf("memory context changed within one Turn: first=%q second=%q", firstMemory, secondMemory)
	}
	for _, message := range history {
		if message.Name == llm.UserNameMemoryContext {
			t.Fatalf("request-only memory context leaked into durable history: %+v", history)
		}
	}
}

func TestTurnCanDisableAutomaticMemoryRecallWithoutRemovingService(t *testing.T) {
	hub := stream.NewHub(32, logx.Discard())
	service, err := memory.OpenLocalService(t.TempDir()+"/agent.db", t.TempDir()+"/global.db", memory.ScopeAgent)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if _, err := service.Remember(context.Background(), memory.RememberRequest{
		Information: "自动召回不应出现", Tier: memory.TierRecall, Kind: memory.KindFact,
	}); err != nil {
		t.Fatal(err)
	}

	client := &snapshotLLM{}
	pol := policy.NewDefaultEngine()
	orch := NewOrchestrator("a1", t.TempDir(), hub, client, testRegistry(t), pol, SkillAccess{}, nil, nil,
		hooks.RuntimeConfig{Duplicate: hooks.DefaultDuplicateConfig(), ToolResult: hooks.DefaultToolResultConfig(t.TempDir())}, logx.Discard())
	orch.SetMemoryService(service)
	orch.SetMemoryAutoRecall(false)

	var history []llm.Message
	if _, _, err := runMessageTurnInline(t, orch, context.Background(), "session-memory-disabled", &history, "继续执行", nil); err != nil {
		t.Fatal(err)
	}
	for requestIndex, request := range client.requests {
		for _, message := range request.Messages {
			if message.Name == llm.UserNameMemoryContext {
				t.Fatalf("request %d unexpectedly contained automatic memory context: %+v", requestIndex, request.Messages)
			}
		}
	}
}

type snapshotLLM struct {
	mu       sync.Mutex
	requests []llm.ChatRequest
	calls    int
}

func (m *snapshotLLM) StreamChat(_ context.Context, req llm.ChatRequest, handler llm.StreamHandler) (llm.ChatResult, error) {
	m.mu.Lock()
	m.requests = append(m.requests, llm.ChatRequest{
		SystemPrompt: req.SystemPrompt,
		Messages:     append([]llm.Message(nil), req.Messages...),
		Tools:        append([]tools.ToolDef(nil), req.Tools...),
	})
	m.calls++
	call := m.calls
	m.mu.Unlock()
	if call == 1 {
		return llm.ChatResult{ToolCalls: []llm.ToolCall{{
			ID: "snapshot-call", Type: "function",
			Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"snapshot.txt"}`},
		}}, FinishReason: "tool_calls"}, nil
	}
	if handler.OnDelta != nil {
		handler.OnDelta("完成")
	}
	return llm.ChatResult{Content: "完成", FinishReason: "stop"}, nil
}

func (m *snapshotLLM) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}

func (m *snapshotLLM) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}
