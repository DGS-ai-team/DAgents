package turn

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/skills"
)

type skillHookStub struct{}

func (skillHookStub) Name() string                 { return hooks.SkillHookNamePrefix + "writer/log" }
func (skillHookStub) Phases() []hooks.Phase        { return []hooks.Phase{hooks.PhaseTurnDone} }
func (skillHookStub) Run(context.Context, *hooks.Context, hooks.Host) (hooks.Result, error) {
	return hooks.Result{Action: hooks.ActionContinue}, nil
}

func TestSyncLoadedSkillHooks_registersAndClears(t *testing.T) {
	root := t.TempDir()
	_ = filepath.Join(root, "writer", "hooks") // skill dir without .so
	catalog := skills.NewCatalog(root, true, 3)
	orch := &Orchestrator{
		toolHooks:   hooks.NewRegistry(nil, hooks.RuntimeConfig{}),
		skillAccess: SkillAccess{Catalog: catalog},
	}
	orch.toolHooks.RegisterPhaseHook(skillHookStub{}, hooks.RegisterOpts{})
	orch.SyncLoadedSkillHooks([]skills.LoadedSkill{{SkillName: "writer"}})
	if len(orch.toolHooks.PhaseHookNames(hooks.PhaseTurnDone)) != 0 {
		t.Fatalf("turn_done hooks = %v, want 0 without .so plugins", orch.toolHooks.PhaseHookNames(hooks.PhaseTurnDone))
	}
	orch.toolHooks.RegisterPhaseHook(skillHookStub{}, hooks.RegisterOpts{})
	orch.SyncLoadedSkillHooks(nil)
	if len(orch.toolHooks.PhaseHookNames(hooks.PhaseTurnDone)) != 0 {
		t.Fatalf("expected hooks cleared after unload")
	}
}
