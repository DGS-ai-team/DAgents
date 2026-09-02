package agentruntime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestBuild_usesAgentWorkspaceAndToolGroups(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{NodeID: "n1", RuntimeRoot: root}
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
		BaseTurn: session.TurnOptions{WorkspaceRoot: root},
		AgentID:  "agt-abc",
		Snapshot: snap,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = built.Close() })
	if built.TurnOptions.MaxToolLoops != DefaultMaxToolLoops {
		t.Fatalf("MaxToolLoops=%d want %d (from agent default, not BaseTurn)", built.TurnOptions.MaxToolLoops, DefaultMaxToolLoops)
	}
	if built.WorkspaceRoot != root || built.TurnOptions.WorkspaceRoot != root {
		t.Fatalf("workspaceRoot=%q turn=%q want %q", built.WorkspaceRoot, built.TurnOptions.WorkspaceRoot, root)
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

func TestBuild_usesCustomWorkspaceRoot(t *testing.T) {
	nodeRoot := t.TempDir()
	workspace := t.TempDir()
	cfg := &config.Config{NodeID: "n1", RuntimeRoot: nodeRoot}
	cfg.ApplyDefaults()
	built, err := Build(BuildParams{
		NodeCFG:  cfg,
		BaseTurn: session.TurnOptions{WorkspaceRoot: nodeRoot},
		AgentID:  "agt-custom",
		Snapshot: Snapshot{Workspace: WorkspaceConfig{Mode: WorkspaceModeCustom, Path: workspace}},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = built.Close() })
	wantWorkspace, err = filepath.Abs(wantWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	if built.WorkspaceRoot != wantWorkspace || built.Registry.WorkspaceRoot() != wantWorkspace {
		t.Fatalf("workspace root built=%q registry=%q want %q", built.WorkspaceRoot, built.Registry.WorkspaceRoot(), wantWorkspace)
	}
	if built.TurnOptions.WorkspaceRoot != wantWorkspace {
		t.Fatalf("turn workspace=%q want %q", built.TurnOptions.WorkspaceRoot, wantWorkspace)
	}
	stateRoot, err := WorkspaceStateRoot(wantWorkspace, "agt-custom")
	if err != nil {
		t.Fatal(err)
	}
	if built.TurnOptions.AgentID != "agt-custom" || built.TurnOptions.WorkspaceStateRoot != stateRoot {
		t.Fatalf("workspace identity/state=%q/%q want %q/%q", built.TurnOptions.AgentID, built.TurnOptions.WorkspaceStateRoot, "agt-custom", stateRoot)
	}
	if built.TurnOptions.RawMessageHistoryDir != filepath.Join(stateRoot, "history") {
		t.Fatalf("history dir=%q want %q", built.TurnOptions.RawMessageHistoryDir, filepath.Join(stateRoot, "history"))
	}
	if built.TurnOptions.RawMessageHistoryRelativeRoot != ".dagents/agt-custom" {
		t.Fatalf("history relative root=%q", built.TurnOptions.RawMessageHistoryRelativeRoot)
	}
	if built.TurnOptions.ToolResult.AgentID != "agt-custom" {
		t.Fatalf("tool result agent id=%q", built.TurnOptions.ToolResult.AgentID)
	}
}

func TestBuild_skillsFollowsToolGroup(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{NodeID: "n1", RuntimeRoot: root}
	cfg.ApplyDefaults()
	cfg.Skills.Enabled = true

	built, err := Build(BuildParams{
		NodeCFG:  cfg,
		BaseTurn: session.TurnOptions{WorkspaceRoot: root, SkillsEnabled: true},
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
	t.Cleanup(func() { _ = built.Close() })
	if built.TurnOptions.SkillsEnabled {
		t.Fatal("skills group absent: SkillsEnabled should be false")
	}
}

func TestBuild_emptyOrMissingToolGroupsMeansNone(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{NodeID: "n1", RuntimeRoot: root}
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
				BaseTurn: session.TurnOptions{WorkspaceRoot: root},
				AgentID:  "agt-none",
				Snapshot: tc.snap,
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = built.Close() })
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
	cfg := &config.Config{NodeID: "n1", RuntimeRoot: root}
	cfg.ApplyDefaults()
	cfg.Skills.Enabled = true

	built, err := Build(BuildParams{
		NodeCFG:  cfg,
		BaseTurn: session.TurnOptions{WorkspaceRoot: root},
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
	t.Cleanup(func() { _ = built.Close() })
	if !built.TurnOptions.SkillsEnabled {
		t.Fatal("legacy defaults.skills.enabled=false must not disable skills when tool group present")
	}
}

func TestBuildSeparatesAutomaticMemoryRecallFromMemoryTools(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{NodeID: "n1", RuntimeRoot: root}
	cfg.ApplyDefaults()

	tests := []struct {
		name           string
		groups         []any
		longTerm       any
		wantService    bool
		wantAutoRecall bool
		wantMemoryTool bool
	}{
		{
			name:           "automatic recall without memory tools",
			groups:         []any{"fs"},
			longTerm:       true,
			wantService:    true,
			wantAutoRecall: true,
			wantMemoryTool: false,
		},
		{
			name:           "memory tools without automatic recall",
			groups:         []any{"memory"},
			longTerm:       false,
			wantService:    true,
			wantAutoRecall: false,
			wantMemoryTool: true,
		},
		{
			name:           "neither capability",
			groups:         []any{"fs"},
			longTerm:       false,
			wantService:    false,
			wantMemoryTool: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			built, err := Build(BuildParams{
				NodeCFG:  cfg,
				BaseTurn: session.TurnOptions{WorkspaceRoot: root},
				AgentID:  "agt-memory-capability",
				Snapshot: Snapshot{Defaults: map[string]any{
					"tools":          map[string]any{"enabled_groups": tc.groups},
					"prompt_context": map[string]any{"long_term_enabled": tc.longTerm},
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = built.Close() })
			if (built.TurnOptions.MemoryService != nil) != tc.wantService {
				t.Fatalf("memory service present=%v want %v", built.TurnOptions.MemoryService != nil, tc.wantService)
			}
			if built.TurnOptions.MemoryAutoRecall != tc.wantAutoRecall {
				t.Fatalf("MemoryAutoRecall=%v want %v", built.TurnOptions.MemoryAutoRecall, tc.wantAutoRecall)
			}
			hasMemoryTool := false
			for _, definition := range built.Registry.Definitions() {
				if definition.Function.Name == "memory_search" {
					hasMemoryTool = true
					break
				}
			}
			if hasMemoryTool != tc.wantMemoryTool {
				t.Fatalf("memory_search present=%v want %v", hasMemoryTool, tc.wantMemoryTool)
			}
		})
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

	cfg := &config.Config{NodeID: "n1", RuntimeRoot: root}
	cfg.ApplyDefaults()
	cfg.Skills.Enabled = true

	built, err := Build(BuildParams{
		NodeCFG: cfg,
		BaseTurn: session.TurnOptions{
			WorkspaceRoot:     root,
			SkillsRoot:        skillsRoot,
			SkillsEnabled:     true,
			SkillsMaxInPrompt: 3,
		},
		AgentID: "agt-skills",
		Snapshot: Snapshot{
			Defaults: map[string]any{
				"tools": map[string]any{"enabled_groups": []any{"skills"}},
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
	t.Cleanup(func() { _ = built.Close() })
	if !built.TurnOptions.SkillsVisibleRestrict {
		t.Fatal("expected visible restrict")
	}
	if len(built.TurnOptions.SkillsVisible) != 1 || built.TurnOptions.SkillsVisible[0] != "keep-me" {
		t.Fatalf("visible=%v", built.TurnOptions.SkillsVisible)
	}
}
