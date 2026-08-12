package turn

import (
	"context"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

type countingCompleteLLM struct {
	calls int
}

func (c *countingCompleteLLM) StreamChat(context.Context, llm.ChatRequest, llm.StreamHandler) (llm.ChatResult, error) {
	return llm.ChatResult{Content: "ok"}, nil
}

func (c *countingCompleteLLM) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	c.calls++
	return "ok", nil
}

func (c *countingCompleteLLM) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func TestResetHookHostLLMQuotaPerHumanTurn(t *testing.T) {
	llmClient := &countingCompleteLLM{}
	orch := NewOrchestrator("agent-1", "/tmp", nil, llmClient, nil, nil, SkillAccess{}, 16, nil, nil, hooks.RuntimeConfig{}, nil)
	orch.SetHookHostConfig(HookHostConfig{MaxLLMCalls: 2})

	var history []llm.Message
	host := orch.newSessionHookHost("sess-1", history, "")
	for i := 0; i < 2; i++ {
		if _, err := host.LLMComplete(context.Background(), hooks.LLMCompleteRequest{UserPrompt: "x"}); err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
	}
	if _, err := host.LLMComplete(context.Background(), hooks.LLMCompleteRequest{UserPrompt: "x"}); err != hooks.ErrLLMQuotaExceeded {
		t.Fatalf("expected quota exceeded within turn, got %v", err)
	}

	orch.resetHookHostLLMQuota()
	host = orch.newSessionHookHost("sess-1", history, "")
	if _, err := host.LLMComplete(context.Background(), hooks.LLMCompleteRequest{UserPrompt: "x"}); err != nil {
		t.Fatalf("expected quota reset for next turn, got %v", err)
	}
}
