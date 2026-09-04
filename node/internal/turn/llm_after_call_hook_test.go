package turn

import (
	"context"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func TestRunLLMAfterCallPhase_mutatesAssistantContent(t *testing.T) {
	orch := NewOrchestrator("ops-01", "/data/ws", nil, nil, nil, nil, SkillAccess{}, nil, nil, hooks.RuntimeConfig{
		Duplicate:  hooks.DefaultDuplicateConfig(),
		ToolResult: hooks.DefaultToolResultConfig("/data/ws"),
	}, nil)
	orch.toolHooks.RegisterPhaseHook(llmAfterCallRedactHook{}, hooks.RegisterOpts{Priority: 0})

	result, err := orch.runLLMAfterCallPhase(context.Background(), "sess-1", llm.ChatResult{
		Content:      "key sk-abcdefghijklmnopqrstuvwxyz1234567890",
		FinishReason: "stop",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content == "key sk-abcdefghijklmnopqrstuvwxyz1234567890" {
		t.Fatalf("expected redaction, got %q", result.Content)
	}
}

type llmAfterCallRedactHook struct{}

func (llmAfterCallRedactHook) Name() string { return "test.llm.redact" }

func (llmAfterCallRedactHook) Phases() []hooks.Phase { return []hooks.Phase{hooks.PhaseLLMAfterCall} }

func (llmAfterCallRedactHook) Run(_ context.Context, hc *hooks.Context, _ hooks.Host) (hooks.Result, error) {
	content := ""
	if hc.LLMAfterCall != nil {
		content = hc.LLMAfterCall.AssistantContent
	}
	return hooks.Result{
		Mutations: map[string]any{
			hooks.MutationAssistantContent: "redacted:" + content,
		},
	}, nil
}
