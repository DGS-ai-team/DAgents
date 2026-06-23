package hooks

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
)

type stubPhaseHook struct {
	name     string
	phases   []Phase
	priority int
	fn       func(ctx context.Context, hc *Context, host Host) (Result, error)
}

func (h stubPhaseHook) Name() string    { return h.name }
func (h stubPhaseHook) Phases() []Phase { return h.phases }
func (h stubPhaseHook) Run(ctx context.Context, hc *Context, host Host) (Result, error) {
	if h.fn != nil {
		return h.fn(ctx, hc, host)
	}
	return Result{Action: ActionContinue}, nil
}

func TestRunPhase_priorityOrder(t *testing.T) {
	var order []string
	reg := NewRegistry(nil, RuntimeConfig{Duplicate: DefaultDuplicateConfig()})
	reg.RegisterPhaseHook(stubPhaseHook{
		name: "b", phases: []Phase{PhasePromptBuild},
		fn: func(_ context.Context, _ *Context, _ Host) (Result, error) {
			order = append(order, "b")
			return Result{}, nil
		},
	}, RegisterOpts{Priority: 20})
	reg.RegisterPhaseHook(stubPhaseHook{
		name: "a", phases: []Phase{PhasePromptBuild},
		fn: func(_ context.Context, _ *Context, _ Host) (Result, error) {
			order = append(order, "a")
			return Result{}, nil
		},
	}, RegisterOpts{Priority: 10})

	out, err := reg.RunPhase(context.Background(), PhasePromptBuild, &Context{
		SessionID: "s1",
		TurnID:    "t1",
		PromptBuild: &PromptBuildPayload{SystemPrompt: "base"},
	}, NoopHost())
	if err != nil {
		t.Fatal(err)
	}
	if out.PromptBuild == nil || out.PromptBuild.SystemPrompt != "base" {
		t.Fatalf("context = %+v", out.PromptBuild)
	}
	if len(order) != 2 || order[0] != "a" || order[1] != "b" {
		t.Fatalf("order = %v", order)
	}
}

func TestRunPhase_appliesMutations(t *testing.T) {
	reg := NewRegistry(nil, RuntimeConfig{Duplicate: DefaultDuplicateConfig()})
	reg.RegisterPhaseHook(stubPhaseHook{
		name:   "inject",
		phases: []Phase{PhasePromptBuild},
		fn: func(_ context.Context, hc *Context, _ Host) (Result, error) {
			base := ""
			if hc.PromptBuild != nil {
				base = hc.PromptBuild.SystemPrompt
			}
			return Result{
				Mutations: map[string]any{
					MutationSystemPrompt: base + "\n## extra",
				},
			}, nil
		},
	}, RegisterOpts{})

	out, err := reg.RunPhase(context.Background(), PhasePromptBuild, &Context{
		TurnID:      "t1",
		PromptBuild: &PromptBuildPayload{SystemPrompt: "root"},
	}, NoopHost())
	if err != nil {
		t.Fatal(err)
	}
	if out.PromptBuild.SystemPrompt != "root\n## extra" {
		t.Fatalf("system prompt = %q", out.PromptBuild.SystemPrompt)
	}
}

func TestRunPhase_actionSkip(t *testing.T) {
	var ranSecond atomic.Bool
	reg := NewRegistry(nil, RuntimeConfig{Duplicate: DefaultDuplicateConfig()})
	reg.RegisterPhaseHook(stubPhaseHook{
		name:   "skipper",
		phases: []Phase{PhaseTurnDone},
		fn: func(_ context.Context, _ *Context, _ Host) (Result, error) {
			return Result{Action: ActionSkip}, nil
		},
	}, RegisterOpts{Priority: 0})
	reg.RegisterPhaseHook(stubPhaseHook{
		name:   "later",
		phases: []Phase{PhaseTurnDone},
		fn: func(_ context.Context, _ *Context, _ Host) (Result, error) {
			ranSecond.Store(true)
			return Result{}, nil
		},
	}, RegisterOpts{Priority: 1})

	_, err := reg.RunPhase(context.Background(), PhaseTurnDone, &Context{TurnID: "t1"}, NoopHost())
	if err != nil {
		t.Fatal(err)
	}
	if ranSecond.Load() {
		t.Fatal("expected second hook skipped")
	}
}

func TestRunPhase_actionAbortTurn(t *testing.T) {
	reg := NewRegistry(nil, RuntimeConfig{Duplicate: DefaultDuplicateConfig()})
	reg.RegisterPhaseHook(stubPhaseHook{
		name:   "guard",
		phases: []Phase{PhaseLLMAfterCall},
		fn: func(_ context.Context, _ *Context, _ Host) (Result, error) {
			return Result{
				Action: ActionAbortTurn,
				Err:    errors.New("policy violation"),
			}, nil
		},
	}, RegisterOpts{})

	_, err := reg.RunPhase(context.Background(), PhaseLLMAfterCall, &Context{TurnID: "t1"}, NoopHost())
	if err == nil {
		t.Fatal("expected abort error")
	}
	var abort *PhaseAbortError
	if !errors.As(err, &abort) {
		t.Fatalf("error = %v", err)
	}
	if abort.Phase != PhaseLLMAfterCall || abort.Action != ActionAbortTurn || abort.Hook != "guard" {
		t.Fatalf("abort = %+v", abort)
	}
}

func TestRunPhase_filtersByPhase(t *testing.T) {
	var ran atomic.Bool
	reg := NewRegistry(nil, RuntimeConfig{Duplicate: DefaultDuplicateConfig()})
	reg.RegisterPhaseHook(stubPhaseHook{
		name:   "prompt-only",
		phases: []Phase{PhasePromptBuild},
		fn: func(_ context.Context, _ *Context, _ Host) (Result, error) {
			ran.Store(true)
			return Result{}, nil
		},
	}, RegisterOpts{})

	_, err := reg.RunPhase(context.Background(), PhaseTurnDone, &Context{TurnID: "t1"}, NoopHost())
	if err != nil {
		t.Fatal(err)
	}
	if ran.Load() {
		t.Fatal("hook for other phase should not run")
	}
}

func TestRunPhase_onErrorContinue(t *testing.T) {
	var ranAfter atomic.Bool
	reg := NewRegistry(nil, RuntimeConfig{Duplicate: DefaultDuplicateConfig()})
	reg.RegisterPhaseHook(stubPhaseHook{
		name:   "fail",
		phases: []Phase{PhaseTurnDone},
		fn: func(_ context.Context, _ *Context, _ Host) (Result, error) {
			return Result{}, errors.New("boom")
		},
	}, RegisterOpts{OnError: OnErrorContinue})
	reg.RegisterPhaseHook(stubPhaseHook{
		name:   "after",
		phases: []Phase{PhaseTurnDone},
		fn: func(_ context.Context, _ *Context, _ Host) (Result, error) {
			ranAfter.Store(true)
			return Result{}, nil
		},
	}, RegisterOpts{Priority: 1})

	_, err := reg.RunPhase(context.Background(), PhaseTurnDone, &Context{TurnID: "t1"}, NoopHost())
	if err != nil {
		t.Fatal(err)
	}
	if !ranAfter.Load() {
		t.Fatal("expected chain to continue after hook error")
	}
}

func TestRunPhase_onErrorAbort(t *testing.T) {
	reg := NewRegistry(nil, RuntimeConfig{Duplicate: DefaultDuplicateConfig()})
	reg.RegisterPhaseHook(stubPhaseHook{
		name:   "fail",
		phases: []Phase{PhaseTurnDone},
		fn: func(_ context.Context, _ *Context, _ Host) (Result, error) {
			return Result{}, errors.New("boom")
		},
	}, RegisterOpts{OnError: OnErrorAbort})

	_, err := reg.RunPhase(context.Background(), PhaseTurnDone, &Context{TurnID: "t1"}, NoopHost())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunPhase_sideEffectJournalSkipsReplay(t *testing.T) {
	var runs atomic.Int32
	reg := NewRegistry(nil, RuntimeConfig{Duplicate: DefaultDuplicateConfig()})
	reg.SetExecutionJournal(NewMemoryExecutionJournal())
	reg.RegisterPhaseHook(stubPhaseHook{
		name:   "once",
		phases: []Phase{PhaseTurnDone},
		fn: func(_ context.Context, _ *Context, _ Host) (Result, error) {
			runs.Add(1)
			return Result{}, nil
		},
	}, RegisterOpts{SideEffect: true})

	hc := &Context{TurnID: "turn-42"}
	if _, err := reg.RunPhase(context.Background(), PhaseTurnDone, hc, NoopHost()); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.RunPhase(context.Background(), PhaseTurnDone, hc, NoopHost()); err != nil {
		t.Fatal(err)
	}
	if runs.Load() != 1 {
		t.Fatalf("runs = %d, want 1", runs.Load())
	}
}

func TestRunPhase_timeoutFailOpen(t *testing.T) {
	reg := NewRegistry(nil, RuntimeConfig{Duplicate: DefaultDuplicateConfig()})
	reg.RegisterPhaseHook(stubPhaseHook{
		name:   "slow",
		phases: []Phase{PhaseTurnDone},
		fn: func(ctx context.Context, _ *Context, _ Host) (Result, error) {
			select {
			case <-ctx.Done():
				return Result{}, ctx.Err()
			case <-time.After(200 * time.Millisecond):
				return Result{}, nil
			}
		},
	}, RegisterOpts{Timeout: 20 * time.Millisecond, OnError: OnErrorContinue})

	_, err := reg.RunPhase(context.Background(), PhaseTurnDone, &Context{TurnID: "t1"}, NoopHost())
	if err != nil {
		t.Fatalf("timeout should fail-open: %v", err)
	}
}

func TestRunPhase_nilRegistry(t *testing.T) {
	var reg *Registry
	out, err := reg.RunPhase(context.Background(), PhaseTurnDone, &Context{
		TurnID:   "t1",
		TurnDone: &TurnDonePayload{FinishReason: "stop"},
	}, NoopHost())
	if err != nil {
		t.Fatal(err)
	}
	if out.TurnDone.FinishReason != "stop" {
		t.Fatalf("out = %+v", out.TurnDone)
	}
}

func TestRunPhase_nilContext(t *testing.T) {
	reg := NewRegistry(nil, RuntimeConfig{Duplicate: DefaultDuplicateConfig()})
	_, err := reg.RunPhase(context.Background(), PhaseTurnDone, nil, NoopHost())
	if err == nil {
		t.Fatal("expected error for nil context")
	}
}

func TestRunPhase_builtinToolBeforeEachChain(t *testing.T) {
	engine, _ := policy.LoadFile("")
	reg := NewRegistry(engine, RuntimeConfig{Duplicate: DefaultDuplicateConfig()})
	out := registryToolBeforeEach(reg, ToolBeforeEachInput{ToolName: "read_file"})
	if out.Action != policy.ActionAuto {
		t.Fatalf("Action = %q", out.Action)
	}
}

func TestApplyMutation_messages(t *testing.T) {
	hc := &Context{}
	msgs := []llm.Message{{Role: "user", Content: "hi"}}
	if err := applyMutations(hc, map[string]any{MutationMessages: msgs}); err != nil {
		t.Fatal(err)
	}
	if len(hc.LLMBeforeCall.Messages) != 1 || hc.LLMBeforeCall.Messages[0].Content != "hi" {
		t.Fatalf("messages = %+v", hc.LLMBeforeCall)
	}
}

func TestActionIsAbort(t *testing.T) {
	if ActionContinue.IsAbort() || ActionSkip.IsAbort() {
		t.Fatal("continue/skip should not abort")
	}
	if !ActionAbortTurn.IsAbort() {
		t.Fatal("abort_turn should abort")
	}
}
