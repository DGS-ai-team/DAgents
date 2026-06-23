package hooks

import (
	"fmt"

	"encoding/json"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
)

// 通用 mutation 键；Registry.applyMutations 按 phase 写入 Context。
const (
	MutationSystemPrompt     = "system_prompt"
	MutationAssistantContent = "assistant_content"
	MutationMessages         = "messages"
	MutationMetadata         = "metadata"
	MutationSkipCompress     = "skip_compress"
	MutationToolAfterEach    = "tool_after_each"
	MutationToolBeforeEach   = "tool_before_each"
	MutationSessionStore     = "session_store"
	MutationHistoryAppend    = "history_append"
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
	case MutationToolAfterEach:
		return applyToolAfterEachMutation(hc, val)
	case MutationToolBeforeEach:
		return applyToolBeforeEachMutation(hc, val)
	case MutationSessionStore:
		return applySessionStoreMutation(hc, val)
	case MutationHistoryAppend:
		msg, ok := val.(llm.Message)
		if !ok {
			return fmt.Errorf("hooks: mutation %q expects llm.Message", key)
		}
		if err := validateHistoryAppendMessage(msg); err != nil {
			return err
		}
		hc.History = append(hc.History, msg)
		return nil
	default:
		return fmt.Errorf("hooks: unknown mutation key %q", key)
	}
}

func applyToolAfterEachMutation(hc *Context, val any) error {
	out, ok := val.(ToolAfterEachOutput)
	if !ok {
		if m, okMap := val.(map[string]any); okMap {
			out = ToolAfterEachOutput{}
			if s, ok := m["for_client"].(string); ok {
				out.ForClient = s
			}
			if s, ok := m["for_history"].(string); ok {
				out.ForHistory = s
			}
		} else {
			return fmt.Errorf("hooks: mutation %q expects ToolAfterEachOutput or map", MutationToolAfterEach)
		}
	}
	target := ensureToolAfterEachOutput(hc)
	target.ForClient = out.ForClient
	target.ForHistory = out.ForHistory
	return nil
}

func applyToolBeforeEachMutation(hc *Context, val any) error {
	m, ok := val.(map[string]any)
	if !ok {
		return fmt.Errorf("hooks: mutation %q expects map[string]any", MutationToolBeforeEach)
	}
	decision := ensureToolDecision(hc)
	if action, ok := m["action"].(string); ok && action != "" {
		decision.Action = policy.Action(action)
	}
	if mode, ok := m["tool_mode"].(string); ok && mode != "" {
		decision.ToolMode = policy.ApprovalMode(mode)
	}
	return nil
}

func applySessionStoreMutation(hc *Context, val any) error {
	patch, ok := val.(map[string]any)
	if !ok {
		return fmt.Errorf("hooks: mutation %q expects map[string]any", MutationSessionStore)
	}
	if hc.SessionStore == nil {
		hc.SessionStore = make(map[string]json.RawMessage)
	}
	for k, v := range patch {
		if v == nil {
			delete(hc.SessionStore, k)
			continue
		}
		raw, err := EncodeSessionStoreValue(v)
		if err != nil {
			return fmt.Errorf("hooks: mutation %q key %q: %w", MutationSessionStore, k, err)
		}
		hc.SessionStore[k] = raw
	}
	return nil
}

func validateHistoryAppendMessage(msg llm.Message) error {
	role := msg.Role
	switch role {
	case "user", "assistant", "tool", "system":
		return nil
	default:
		return fmt.Errorf("hooks: history_append unsupported role %q", role)
	}
}
