package agentruntime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestBuild_sandboxIsolation(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{NodeID: "n1", FSRoot: root}
	cfg.ApplyDefaults()
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
}

func TestBuild_appliesSkillsVisibleAllowlist(t *testing.T) {
	root := t.TempDir()
	skillsRoot := filepath.Join(root, "skills")
	if err := os.MkdirAll(filepath.Join(skillsRoot, "keep-me"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillsRoot, "keep-me", "SKILL.md"), []byte("---\nname: keep-me\ndescription: Keep\n---\nBody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(skillsRoot, "hide-me"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillsRoot, "hide-me", "SKILL.md"), []byte("---\nname: hide-me\ndescription: Hide\n---\nBody\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{NodeID: "n1", FSRoot: root}
	cfg.ApplyDefaults()
	cfg.Skills.Enabled = true

	built, err := Build(BuildParams{
		NodeCFG: cfg,
		BaseTurn: session.TurnOptions{
			FSRoot:            root,
			SkillsRoot:        skillsRoot,
			SkillsEnabled:     true,
			SkillsMaxInPrompt: 3,
		},
		AgentID: "agt-skills",
		Snapshot: Snapshot{
			Defaults: map[string]any{
				"skills": map[string]any{
					"enabled": true,
					"visible": []any{"keep-me"},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !built.TurnOptions.SkillsVisibleRestrict {
		t.Fatal("expected visible restrict")
	}
	if len(built.TurnOptions.SkillsVisible) != 1 || built.TurnOptions.SkillsVisible[0] != "keep-me" {
		t.Fatalf("visible=%v", built.TurnOptions.SkillsVisible)
	}
}
