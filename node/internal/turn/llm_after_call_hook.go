package turn

import (
	"context"
	"errors"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func chatResultFromLLMAfterCall(in hooks.LLMAfterCallInput) llm.ChatResult {
	return llm.ChatResult{
		Content:      in.AssistantContent,
		ToolCalls:    append([]llm.ToolCall(nil), in.ToolCalls...),
		FinishReason: in.FinishReason,
	}
}

func llmAfterCallInputFromResult(result llm.ChatResult) hooks.LLMAfterCallInput {
	return hooks.LLMAfterCallInput{
		AssistantContent: result.Content,
		ToolCalls:        append([]llm.ToolCall(nil), result.ToolCalls...),
		FinishReason:     result.FinishReason,
	}
}

func (o *Orchestrator) runLLMAfterCallPhase(ctx context.Context, sessionID string, result llm.ChatResult) (llm.ChatResult, error) {
	if o.toolHooks == nil {
		return result, nil
	}
	in := llmAfterCallInputFromResult(result)
	hc := hooks.BuildLLMAfterCallContext(sessionID, o.agentID, in)
	out, err := o.toolHooks.RunPhase(ctx, hooks.PhaseLLMAfterCall, hc)
	if err != nil {
		return result, err
	}
	merged := hooks.ApplyLLMAfterCallToResult(out, in)
	return chatResultFromLLMAfterCall(merged), nil
}

func isLLMAfterCallAbort(err error) bool {
	var abort *hooks.PhaseAbortError
	return errors.As(err, &abort) && abort.Phase == hooks.PhaseLLMAfterCall
}
