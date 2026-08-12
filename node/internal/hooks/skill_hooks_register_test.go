package hooks

import (
	"testing"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestRemovePhaseHooksByPrefix(t *testing.T) {
	reg := NewRegistry(nil, RuntimeConfig{})
	reg.RegisterPhaseHook(stubPhaseHook{name: "skill/demo/a", phases: []Phase{PhaseTurnDone}}, RegisterOpts{})
	reg.RegisterPhaseHook(stubPhaseHook{name: "global/x", phases: []Phase{PhaseTurnDone}}, RegisterOpts{})
	if len(reg.phaseHooksFor(PhaseTurnDone)) != 2 {
		t.Fatalf("before remove = %d", len(reg.phaseHooksFor(PhaseTurnDone)))
	}
	reg.RemovePhaseHooksByPrefix("skill/")
	if len(reg.phaseHooksFor(PhaseTurnDone)) != 1 {
		t.Fatalf("after remove = %d", len(reg.phaseHooksFor(PhaseTurnDone)))
	}
	if reg.phaseHooksFor(PhaseTurnDone)[0].hook.Name() != "global/x" {
		t.Fatalf("remaining = %q", reg.phaseHooksFor(PhaseTurnDone)[0].hook.Name())
	}
}

func TestPluginHookEntryFromConfig(t *testing.T) {
	entry, ok := PluginHookEntryFromConfig(config.HookPluginConfig{
		Path: ".runtime/plugins/demo.so", Phases: []string{"turn.done"},
	}, "/runtime")
	if !ok || entry.Path != "/runtime/.runtime/plugins/demo.so" || len(entry.Phases) != 1 {
		t.Fatalf("entry = %+v ok=%v", entry, ok)
	}
	_, ok = PluginHookEntryFromConfig(config.HookPluginConfig{
		Phases: []string{"turn.done"},
	}, "")
	if ok {
		t.Fatal("expected invalid without path")
	}
}

func TestPluginsConfigFromShared(t *testing.T) {
	cfg := PluginsConfigFromShared(config.HooksConfig{
		Plugins: []config.HookPluginConfig{{
			Path:   "plugins/foo.so",
			Phases: []string{"tool.after_each"},
		}},
	}, "/rt")
	if len(cfg.Plugins) != 1 || cfg.Plugins[0].Path != "/rt/plugins/foo.so" {
		t.Fatalf("cfg = %+v", cfg)
	}
}
