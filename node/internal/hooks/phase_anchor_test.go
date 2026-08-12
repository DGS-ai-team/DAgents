package hooks

import (
	"context"
	"strings"
	"testing"
)

func TestRunPhase_promptBuildMutation(t *testing.T) {
	reg := NewRegistry(nil, RuntimeConfig{Duplicate: DefaultDuplicateConfig()})
	reg.RegisterPhaseHook(stubPhaseHook{
		name:   "inject.enterprise",
		phases: []Phase{PhasePromptBuild},
		fn: func(_ context.Context, hc *Context, _ Host) (Result, error) {
			base := ""
			if hc.PromptBuild != nil {
				base = hc.PromptBuild.SystemPrompt
			}
			return Result{
				Mutations: map[string]any{
					MutationSystemPrompt: base + "\n## Enterprise Policy",
				},
			}, nil
		},
	}, RegisterOpts{Priority: 0})

	hc := BuildPromptBuildContext("sess-1", "agent-1", "base prompt")
	out, err := reg.RunPhase(context.Background(), PhasePromptBuild, hc, NoopHost())
	if err != nil {
		t.Fatal(err)
	}
	got := SystemPromptFrom(out, "fallback")
	want := "base prompt\n## Enterprise Policy"
	if got != want {
		t.Fatalf("prompt = %q, want %q", got, want)
	}
}

func TestRunPhase_turnDoneObservational(t *testing.T) {
	reg := NewRegistry(nil, RuntimeConfig{Duplicate: DefaultDuplicateConfig()})
	var captured string
	reg.RegisterPhaseHook(stubPhaseHook{
		name:   "metrics.stub",
		phases: []Phase{PhaseTurnDone},
		fn: func(_ context.Context, hc *Context, _ Host) (Result, error) {
			if hc.TurnDone != nil {
				captured = hc.TurnDone.FinishReason
			}
			return Result{}, nil
		},
	}, RegisterOpts{Priority: 0})

	hc := BuildTurnDoneContext("sess-2", "agent-2", "stop")
	if _, err := reg.RunPhase(context.Background(), PhaseTurnDone, hc, NoopHost()); err != nil {
		t.Fatal(err)
	}
	if captured != "stop" {
		t.Fatalf("finish_reason = %q", captured)
	}
}

func TestSystemPromptFrom_emptyMutationKeepsFallback(t *testing.T) {
	hc := Context{PromptBuild: &PromptBuildPayload{SystemPrompt: ""}}
	if got := SystemPromptFrom(hc, "fallback"); got != "fallback" {
		t.Fatalf("got %q", got)
	}
}

func TestBuildPromptBuildContext_setsPhase(t *testing.T) {
	hc := BuildPromptBuildContext("s", "a", "p")
	if hc.Phase != PhasePromptBuild || hc.PromptBuild.SystemPrompt != "p" {
		t.Fatalf("context = %+v", hc)
	}
}

func TestRunPhase_promptBuildNoExtraHooksIsNoop(t *testing.T) {
	reg := NewRegistry(nil, RuntimeConfig{Duplicate: DefaultDuplicateConfig()})
	before := reg.phaseHooksFor(PhasePromptBuild)
	if len(before) != 0 {
		t.Fatalf("expected no prompt.build hooks by default, got %d", len(before))
	}
	raw := strings.Repeat("x", 64)
	hc := BuildPromptBuildContext("s", "a", raw)
	out, err := reg.RunPhase(context.Background(), PhasePromptBuild, hc, NoopHost())
	if err != nil {
		t.Fatal(err)
	}
	if SystemPromptFrom(out, "") != raw {
		t.Fatal("prompt should be unchanged")
	}
}

func TestRunPhase_llmAfterCallMutation(t *testing.T) {
	reg := NewRegistry(nil, RuntimeConfig{Duplicate: DefaultDuplicateConfig()})
	reg.RegisterPhaseHook(stubPhaseHook{
		name:   "redact.stub",
		phases: []Phase{PhaseLLMAfterCall},
		fn: func(_ context.Context, hc *Context, _ Host) (Result, error) {
			content := ""
			if hc.LLMAfterCall != nil {
				content = hc.LLMAfterCall.AssistantContent
			}
			return Result{
				Mutations: map[string]any{
					MutationAssistantContent: strings.ReplaceAll(content, "SECRET", "[REDACTED]"),
				},
			}, nil
		},
	}, RegisterOpts{Priority: 0})

	hc := BuildLLMAfterCallContext("s1", "a1", LLMAfterCallInput{
		AssistantContent: "leaked SECRET token",
		FinishReason:     "stop",
	})
	out, err := reg.RunPhase(context.Background(), PhaseLLMAfterCall, hc, NoopHost())
	if err != nil {
		t.Fatal(err)
	}
	merged := ApplyLLMAfterCallToResult(out, LLMAfterCallInput{AssistantContent: "leaked SECRET token"})
	if merged.AssistantContent != "leaked [REDACTED] token" {
		t.Fatalf("content = %q", merged.AssistantContent)
	}
}

func TestRunPhase_llmAfterCallNoHooksIsNoop(t *testing.T) {
	reg := NewRegistry(nil, RuntimeConfig{Duplicate: DefaultDuplicateConfig()})
	if len(reg.phaseHooksFor(PhaseLLMAfterCall)) != 0 {
		t.Fatal("expected no llm.after_call hooks by default")
	}
	in := LLMAfterCallInput{AssistantContent: "hello", FinishReason: "stop"}
	hc := BuildLLMAfterCallContext("s", "a", in)
	out, err := reg.RunPhase(context.Background(), PhaseLLMAfterCall, hc, NoopHost())
	if err != nil {
		t.Fatal(err)
	}
	merged := ApplyLLMAfterCallToResult(out, in)
	if merged.AssistantContent != "hello" {
		t.Fatalf("content = %q", merged.AssistantContent)
	}
}
