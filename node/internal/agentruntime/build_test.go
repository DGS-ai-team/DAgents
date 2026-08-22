package agentruntime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestBuild_usesNodeFSRootAndToolGroups(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{NodeID: "n1", FSRoot: root}
	cfg.ApplyDefaults()
	cfg.Skills.Enabled = true

	snap := Snapshot{
		TemplateID: "code-reviewer",
		Defaults: map[string]any{
			"tools": map[string]any{"enabled_groups": []any{"fs", "skills"}},
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
	if built.FSRoot != root || built.TurnOptions.FSRoot != root {
		t.Fatalf("fsRoot=%q turn=%q want %q", built.FSRoot, built.TurnOptions.FSRoot, root)
	}
	if len(built.ToolGroups) != 2 || built.ToolGroups[0] != "fs" || built.ToolGroups[1] != "skills" {
		t.Fatalf("tool groups=%v", built.ToolGroups)
	}
	if built.Registry == nil {
		t.Fatal("nil registry")
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

func TestBuild_emptyOrMissingToolGroupsMeansNone(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{NodeID: "n1", FSRoot: root}
	cfg.ApplyDefaults()

	for _, tc := range []struct {
		name string
		snap Snapshot
	}{
		{
			name: "explicit empty",
			snap: Snapshot{Defaults: map[string]any{
				"tools": map[string]any{"enabled_groups": []any{}},
			}},
		},
		{
			name: "missing field ignores node defaults",
			snap: Snapshot{Defaults: map[string]any{}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			built, err := Build(BuildParams{
				NodeCFG:  cfg,
				BaseTurn: session.TurnOptions{FSRoot: root},
				AgentID:  "agt-none",
				Snapshot: tc.snap,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(built.ToolGroups) != 0 {
				t.Fatalf("tool groups=%v want empty", built.ToolGroups)
			}
			if built.TurnOptions.SkillsEnabled {
				t.Fatal("empty/missing groups must not enable skills")
			}
			for _, d := range built.Registry.Definitions() {
				name := d.Function.Name
				if name == "read_file" || name == "bash" {
					t.Fatalf("must disable builtins, still have %q", name)
				}
			}
		})
	}
}

func TestBuild_ignoresLegacySkillsEnabledFalse(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{NodeID: "n1", FSRoot: root}
	cfg.ApplyDefaults()
	cfg.Skills.Enabled = true

	built, err := Build(BuildParams{
		NodeCFG:  cfg,
		BaseTurn: session.TurnOptions{FSRoot: root},
		AgentID:  "agt-legacy-skills",
		Snapshot: Snapshot{
			Defaults: map[string]any{
				"tools":  map[string]any{"enabled_groups": []any{"fs", "skills"}},
				"skills": map[string]any{"enabled": false},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !built.TurnOptions.SkillsEnabled {
		t.Fatal("legacy defaults.skills.enabled=false must not disable skills when tool group present")
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
				"tools": map[string]any{"enabled_groups": []any{"skills"}},
				"skills": map[string]any{
					"enabled":           true,
					"catalog_tool_mode": true,
					"visible":           []any{"keep-me"},
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
	if !built.TurnOptions.SkillsCatalogToolMode {
		t.Fatal("expected explicit catalog_tool_mode=true to reach TurnOptions")
	}
}
