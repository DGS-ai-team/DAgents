package turn

import (
	"context"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
)

const systemPromptBuildHookName = "builtin.system_prompt"

type systemPromptBuildHook struct {
	compose func(sessionID string) string
}

func newSystemPromptBuildHook(compose func(sessionID string) string) hooks.Hook {
	return &systemPromptBuildHook{compose: compose}
}

func (h *systemPromptBuildHook) Name() string { return systemPromptBuildHookName }

func (h *systemPromptBuildHook) Phases() []hooks.Phase { return []hooks.Phase{hooks.PhasePromptBuild} }

func (h *systemPromptBuildHook) Run(_ context.Context, hc *hooks.Context) (hooks.Result, error) {
	if h == nil || h.compose == nil || hc == nil {
		return hooks.Result{Action: hooks.ActionContinue}, nil
	}
	prompt := h.compose(hc.SessionID)
	return hooks.Result{
		Action: hooks.ActionContinue,
		Mutations: map[string]any{
			hooks.MutationSystemPrompt: prompt,
		},
	}, nil
}

func registerSystemPromptBuildHook(o *Orchestrator) {
	if o == nil || o.toolHooks == nil {
		return
	}
	o.toolHooks.RegisterPhaseHook(newSystemPromptBuildHook(o.composeSystemPrompt), hooks.RegisterOpts{
		Priority: hooks.PromptBuildPriorityBuiltin(),
		Timeout:  hooks.DefaultInlineHookTimeout,
		OnError:  hooks.OnErrorContinue,
	})
}
