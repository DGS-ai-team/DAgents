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
		BaseTurn: session.TurnOptions{FSRoot: root, MaxToolLoops: 8},
		AgentID:  "agt-abc",
		Snapshot: snap,
	})
	if err != nil {
		t.Fatal(err)
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
