package agentruntime

import (
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestBuild_sandboxIsolation(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{NodeID: "n1", FSRoot: root}
	cfg.ApplyDefaults()
	cfg.Skills.Enabled = true
	cfg.Tools.EnabledGroups = []string{"fs", "bash", "browser", "skills"}

	snap := Snapshot{
		TemplateID: "code-reviewer",
		Defaults: map[string]any{
			"tools": map[string]any{"enabled_groups": []any{"fs", "bash", "skills"}},
		},
		Sandbox: SandboxSpec{
			Enabled:           true,
			Backend:           "process",
			WorkspaceSubdir:   "data",
			FSRootIsolation:   true,
			AllowBash:         false,
			AllowNetworkTools: false,
		},
	}
	built, err := Build(BuildParams{
		NodeCFG:  cfg,
		BaseTurn: session.TurnOptions{FSRoot: root},
		AgentID:  "agt-abc",
		Snapshot: snap,
	})
	if err != nil {
		t.Fatal(err)
	}
	if built.TurnOptions.MaxToolLoops != DefaultMaxToolLoops {
		t.Fatalf("MaxToolLoops=%d want %d (from agent default, not BaseTurn)", built.TurnOptions.MaxToolLoops, DefaultMaxToolLoops)
	}
	wantFS := filepath.Join(root, "agents", "agt-abc", "data")
	if built.FSRoot != wantFS || built.TurnOptions.FSRoot != wantFS {
		t.Fatalf("fsRoot=%q turn=%q want %q", built.FSRoot, built.TurnOptions.FSRoot, wantFS)
	}
	for _, g := range built.ToolGroups {
		if g == "bash" || g == "browser" || g == "a2a" {
			t.Fatalf("unexpected group %q in %v", g, built.ToolGroups)
		}
	}
	if built.Registry == nil {
		t.Fatal("nil registry")
	}
	// 校验 bash 组已被沙箱约束去掉。
	for _, g := range built.ToolGroups {
		if g == "bash" {
			t.Fatalf("bash still in groups: %v", built.ToolGroups)
		}
	}
	if !built.TurnOptions.SkillsEnabled {
		t.Fatal("expected SkillsEnabled when skills group present and node skills enabled")
	}
}

func TestBuild_skillsFollowsToolGroup(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{NodeID: "n1", FSRoot: root}
	cfg.ApplyDefaults()
	cfg.Skills.Enabled = true
	cfg.Tools.EnabledGroups = []string{"fs", "bash", "skills"}

	built, err := Build(BuildParams{
		NodeCFG:  cfg,
		BaseTurn: session.TurnOptions{FSRoot: root, SkillsEnabled: true},
		AgentID:  "agt-skills",
		Snapshot: Snapshot{
			Defaults: map[string]any{
				"tools": map[string]any{"enabled_groups": []any{"fs", "bash"}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if built.TurnOptions.SkillsEnabled {
		t.Fatal("skills group absent: SkillsEnabled should be false")
	}
}
