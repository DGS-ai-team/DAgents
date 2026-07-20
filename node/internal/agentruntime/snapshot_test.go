package agentruntime

import (
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestEffectiveFSRoot_isolation(t *testing.T) {
	snap := Snapshot{Sandbox: SandboxSpec{Enabled: true, FSRootIsolation: true, WorkspaceSubdir: "data"}}
	got := EffectiveFSRoot("/rt", "agt-1", snap)
	want := "/rt/agents/agt-1/data"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestEffectiveFSRoot_sharedWhenDisabled(t *testing.T) {
	snap := Snapshot{Sandbox: SandboxSpec{Enabled: false, FSRootIsolation: true}}
	got := EffectiveFSRoot("/rt", "agt-1", snap)
	if got != "/rt" {
		t.Fatalf("got %q", got)
	}
}

func TestApplySandboxToolConstraints(t *testing.T) {
	snap := Snapshot{Sandbox: SandboxSpec{Enabled: true, AllowBash: false, AllowNetworkTools: false}}
	got := ApplySandboxToolConstraints([]string{"fs", "bash", "browser", "skills"}, snap)
	joined := strings.Join(got, ",")
	if strings.Contains(joined, "bash") || strings.Contains(joined, "browser") {
		t.Fatalf("got %v", got)
	}
	if !strings.Contains(joined, "fs") || !strings.Contains(joined, "skills") {
		t.Fatalf("got %v", got)
	}
}

func TestApplySandboxToolConstraints_nilMeansAllThenFilter(t *testing.T) {
	snap := Snapshot{Sandbox: SandboxSpec{Enabled: true, AllowBash: false, AllowNetworkTools: false}}
	got := ApplySandboxToolConstraints(nil, snap)
	for _, g := range got {
		if g == "bash" || g == "browser" || g == "a2a" {
			t.Fatalf("unexpected group %q in %v", g, got)
		}
	}
	all := config.AllBuiltinToolGroupNames()
	if len(got) >= len(all) {
		t.Fatalf("expected filtered smaller than all: %d vs %d", len(got), len(all))
	}
}

func TestEnabledToolGroups(t *testing.T) {
	snap := Snapshot{Defaults: map[string]any{
		"tools": map[string]any{"enabled_groups": []any{"fs", "skills"}},
	}}
	got := EnabledToolGroups(snap)
	if len(got) != 2 || got[0] != "fs" {
		t.Fatalf("got %v", got)
	}
}

func TestParseSnapshot(t *testing.T) {
	raw := []byte(`{"template_id":"x","sandbox":{"enabled":true,"fs_root_isolation":true},"defaults":{"tools":{"enabled_groups":["fs"]}}}`)
	snap, err := ParseSnapshot(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Sandbox.Enabled || snap.Sandbox.Backend != "process" || snap.Sandbox.WorkspaceSubdir != "data" {
		t.Fatalf("%+v", snap.Sandbox)
	}
}
