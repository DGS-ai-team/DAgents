package turn

import (
	"context"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

type riskHostProbeClient struct{ request llm.CompleteRequest }

func (c *riskHostProbeClient) StreamChat(context.Context, llm.ChatRequest, llm.StreamHandler) (llm.ChatResult, error) {
	return llm.ChatResult{}, nil
}
func (c *riskHostProbeClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}
func (c *riskHostProbeClient) CompleteTextWithUsage(_ context.Context, req llm.CompleteRequest) (string, *llm.Usage, error) {
	c.request = req
	return "{}", &llm.Usage{TotalTokens: 3}, nil
}
func (c *riskHostProbeClient) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func TestRiskLLMHostUsesFixedSystemPromptAndUsage(t *testing.T) {
	client := &riskHostProbeClient{}
	host := RiskLLMHost{Client: client}
	resp, err := host.LLMComplete(context.Background(), hooks.LLMCompleteRequest{UserPrompt: "untrusted", MaxOutputTokens: 17})
	if err != nil || resp.Usage == nil || resp.Usage.TotalTokens != 3 {
		t.Fatalf("resp=%+v err=%v", resp, err)
	}
	if client.request.SystemPrompt != DefaultRiskSystemPrompt || client.request.MaxOutputTokens != 17 {
		t.Fatalf("request=%+v", client.request)
	}
}
