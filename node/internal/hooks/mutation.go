package hooks

import (
	"fmt"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

// 通用 mutation 键；Registry.applyMutations 按 phase 写入 Context。
const (
	MutationSystemPrompt     = "system_prompt"
	MutationAssistantContent = "assistant_content"
	MutationMessages         = "messages"
	MutationMetadata         = "metadata"
	MutationSkipCompress     = "skip_compress"
)

func applyMutations(hc *Context, mutations map[string]any) error {
	if hc == nil || len(mutations) == 0 {
		return nil
	}
	for key, val := range mutations {
		if err := applyMutation(hc, key, val); err != nil {
			return err
		}
	}
	return nil
}

func applyMutation(hc *Context, key string, val any) error {
	switch key {
	case MutationSystemPrompt:
		s, ok := val.(string)
		if !ok {
			return fmt.Errorf("hooks: mutation %q expects string", key)
		}
		if hc.PromptBuild == nil {
			hc.PromptBuild = &PromptBuildPayload{}
		}
		hc.PromptBuild.SystemPrompt = s
		return nil
	case MutationAssistantContent:
		s, ok := val.(string)
		if !ok {
			return fmt.Errorf("hooks: mutation %q expects string", key)
		}
		if hc.LLMAfterCall == nil {
			hc.LLMAfterCall = &LLMAfterCallPayload{}
		}
		hc.LLMAfterCall.AssistantContent = s
		return nil
	case MutationMessages:
		msgs, ok := val.([]llm.Message)
		if !ok {
			return fmt.Errorf("hooks: mutation %q expects []llm.Message", key)
		}
		if hc.LLMBeforeCall == nil {
			hc.LLMBeforeCall = &LLMCallPayload{}
		}
		hc.LLMBeforeCall.Messages = append([]llm.Message(nil), msgs...)
		return nil
	case MutationMetadata:
		meta, ok := val.(map[string]any)
		if !ok {
			return fmt.Errorf("hooks: mutation %q expects map[string]any", key)
		}
		if hc.Metadata == nil {
			hc.Metadata = make(map[string]any, len(meta))
		}
		for k, v := range meta {
			hc.Metadata[k] = v
		}
		return nil
	case MutationSkipCompress:
		b, ok := val.(bool)
		if !ok {
			return fmt.Errorf("hooks: mutation %q expects bool", key)
		}
		if hc.TurnBeforeCompress == nil {
			hc.TurnBeforeCompress = &TurnBeforeCompressPayload{}
		}
		hc.TurnBeforeCompress.SkipCompress = b
		return nil
	default:
		return fmt.Errorf("hooks: unknown mutation key %q", key)
	}
}
