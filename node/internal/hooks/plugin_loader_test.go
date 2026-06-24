package hooks

import (
	"context"
	"testing"
)

type multiPhaseStubHook struct {
	name string
}

func (h multiPhaseStubHook) Name() string { return h.name }

func (h multiPhaseStubHook) Phases() []Phase {
	return []Phase{PhaseTurnDone, PhasePromptBuild}
}

func (h multiPhaseStubHook) Run(context.Context, *Context, Host) (Result, error) {
	return Result{Action: ActionContinue}, nil
}

func TestPluginRegistrarConstrainsPhasesFromConfig(t *testing.T) {
	reg := NewRegistry(nil, RuntimeConfig{})
	pluginReg := &PluginRegistrar{
		registry:      reg,
		allowedPhases: []Phase{PhaseTurnDone},
	}
	pluginReg.Register(multiPhaseStubHook{name: "demo"}, RegisterOpts{})

	if len(reg.phaseHooksFor(PhaseTurnDone)) != 1 {
		t.Fatalf("turn.done hooks = %d, want 1", len(reg.phaseHooksFor(PhaseTurnDone)))
	}
	if len(reg.phaseHooksFor(PhasePromptBuild)) != 0 {
		t.Fatalf("prompt.build hooks = %d, want 0", len(reg.phaseHooksFor(PhasePromptBuild)))
	}
}

func TestPluginRegistrarWithoutAllowedPhasesUsesHookPhases(t *testing.T) {
	reg := NewRegistry(nil, RuntimeConfig{})
	pluginReg := &PluginRegistrar{registry: reg}
	pluginReg.Register(multiPhaseStubHook{name: "demo"}, RegisterOpts{})

	if len(reg.phaseHooksFor(PhaseTurnDone)) != 1 {
		t.Fatalf("turn.done hooks = %d, want 1", len(reg.phaseHooksFor(PhaseTurnDone)))
	}
	if len(reg.phaseHooksFor(PhasePromptBuild)) != 1 {
		t.Fatalf("prompt.build hooks = %d, want 1", len(reg.phaseHooksFor(PhasePromptBuild)))
	}
}
