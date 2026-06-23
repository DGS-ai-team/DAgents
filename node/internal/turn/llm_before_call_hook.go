package turn

import (
	"context"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

func (o *Orchestrator) runLLMBeforeCallPhase(ctx context.Context, sessionID string, history *[]llm.Message, systemPrompt string, toolDefs []tools.ToolDef) ([]llm.Message, string, error) {
	if o.toolHooks == nil {
		return *history, systemPrompt, nil
	}
	msgs := append([]llm.Message(nil), *history...)
	hc := hooks.BuildLLMBeforeCallContext(sessionID, o.agentID, msgs, systemPrompt)
	out, err := o.runPhase(ctx, hooks.PhaseLLMBeforeCall, hc, sessionID, history, "")
	if err != nil {
		return *history, systemPrompt, err
	}
	msgsOut := msgs
	if out.LLMBeforeCall != nil && len(out.LLMBeforeCall.Messages) > 0 {
		msgsOut = append([]llm.Message(nil), out.LLMBeforeCall.Messages...)
	}
	promptOut := systemPrompt
	if out.PromptBuild != nil && out.PromptBuild.SystemPrompt != "" {
		promptOut = out.PromptBuild.SystemPrompt
	} else if out.SystemPrompt != "" {
		promptOut = out.SystemPrompt
	}
	return msgsOut, promptOut, nil
}
