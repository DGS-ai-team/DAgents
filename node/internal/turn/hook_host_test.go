package turn

import (
	"context"
	"errors"
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

func (c *countingCompleteLLM) CompleteTextWithUsage(context.Context, llm.CompleteRequest) (string, *llm.Usage, error) {
	c.calls++
	return "ok", &llm.Usage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5}, nil
}

func (c *countingCompleteLLM) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func TestResetHookHostLLMQuotaPerHumanTurn(t *testing.T) {
	llmClient := &countingCompleteLLM{}
	orch := NewOrchestrator("agent-1", "/tmp", nil, llmClient, nil, nil, SkillAccess{}, nil, nil, hooks.RuntimeConfig{}, nil)
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

func TestHookHostUsesCompletionUsageExtension(t *testing.T) {
	client := &countingCompleteLLM{}
	orch := NewOrchestrator("agent-1", ".", nil, client, nil, nil, SkillAccess{}, nil, nil, hooks.RuntimeConfig{}, nil)
	host := orch.newSessionHookHost("sess-1", nil, "")
	resp, err := host.LLMComplete(context.Background(), hooks.LLMCompleteRequest{UserPrompt: "risk"})
	if err != nil || resp.Usage == nil || resp.Usage.TotalTokens != 5 {
		t.Fatalf("response=%+v err=%v", resp, err)
	}
}

type usageErrorLLM struct{}

func (usageErrorLLM) StreamChat(context.Context, llm.ChatRequest, llm.StreamHandler) (llm.ChatResult, error) {
	return llm.ChatResult{}, nil
}
func (usageErrorLLM) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", errors.New("complete failed")
}
func (usageErrorLLM) CompleteTextWithUsage(context.Context, llm.CompleteRequest) (string, *llm.Usage, error) {
	return "partial", &llm.Usage{PromptTokens: 4, CompletionTokens: 1, TotalTokens: 5}, errors.New("complete failed")
}
func (usageErrorLLM) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func TestHookHostPreservesUsageWhenCompletionReturnsError(t *testing.T) {
	orch := NewOrchestrator("agent-1", ".", nil, usageErrorLLM{}, nil, nil, SkillAccess{}, nil, nil, hooks.RuntimeConfig{}, nil)
	host := orch.newSessionHookHost("sess-1", nil, "")
	resp, err := host.LLMComplete(context.Background(), hooks.LLMCompleteRequest{UserPrompt: "risk"})
	if err == nil || resp.Usage == nil || resp.Usage.TotalTokens != 5 || resp.Text != "partial" {
		t.Fatalf("response=%+v err=%v", resp, err)
	}
}
