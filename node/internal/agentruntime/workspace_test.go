package agentruntime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspace_privateAndLegacy(t *testing.T) {
	root := t.TempDir()
	private, err := EffectiveWorkspaceRoot(root, "agt-1", WorkspaceConfig{Mode: WorkspaceModePrivate})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "agents", "agt-1", "workspace")
	if private != want {
		t.Fatalf("private=%q want %q", private, want)
	}
	legacy, err := EffectiveWorkspaceRoot(root, "agt-1", WorkspaceConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if legacy != root {
		t.Fatalf("legacy=%q want %q", legacy, root)
	}
}

func TestNormalizeWorkspace_customCanonicalAndRejectsRuntime(t *testing.T) {
	root := t.TempDir()
	custom := filepath.Join(t.TempDir(), "project")
	got, err := NormalizeWorkspaceConfig(root, "agt-1", WorkspaceConfig{Mode: WorkspaceModeCustom, Path: custom})
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != WorkspaceModeCustom || got.Path == "" {
		t.Fatalf("workspace=%+v", got)
	}
	if info, err := os.Stat(got.Path); err != nil || !info.IsDir() {
		t.Fatalf("custom path was not created: %q err=%v", got.Path, err)
	}
	inside := filepath.Join(root, "outside")
	if _, err := NormalizeWorkspaceConfig(root, "agt-1", WorkspaceConfig{Mode: WorkspaceModeCustom, Path: inside}); err == nil {
		t.Fatal("expected Node runtime path to be rejected")
	}
	if _, err := NormalizeWorkspaceConfig(root, "agt-1", WorkspaceConfig{Mode: WorkspaceModeCustom, Path: "relative/project"}); err == nil {
		t.Fatal("expected relative path to be rejected")
	}
}

func TestEnsureWorkspace_doesNotCreateCompatibilityData(t *testing.T) {
	runtimeRoot := t.TempDir()
	workspace := filepath.Join(t.TempDir(), "project")
	got, err := EnsureWorkspace(runtimeRoot, "agt-1", WorkspaceConfig{
		Mode: WorkspaceModeCustom,
		Path: workspace,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(got, "data")); !os.IsNotExist(err) {
		t.Fatalf("compatibility data directory should not be created, stat error=%v", err)
	}
}

func TestParseSnapshot_workspacePersists(t *testing.T) {
	snap, err := ParseSnapshot([]byte(`{"workspace":{"mode":"custom","path":"C:\\project"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if snap.Workspace.Mode != WorkspaceModeCustom || snap.Workspace.Path != `C:\project` {
		t.Fatalf("workspace=%+v", snap.Workspace)
	}
}

func TestWorkspaceStateRoot_isolatedByAgentID(t *testing.T) {
	workspace := t.TempDir()
	first, err := WorkspaceStateRoot(workspace, "agt-a")
	if err != nil {
		t.Fatal(err)
	}
	second, err := WorkspaceStateRoot(workspace, "agt-b")
	if err != nil {
		t.Fatal(err)
	}
	if first == second || filepath.Dir(first) != filepath.Dir(second) {
		t.Fatalf("state roots=%q, %q want shared parent with distinct agent namespaces", first, second)
	}
	if got, want := filepath.ToSlash(first), filepath.ToSlash(filepath.Join(workspace, ".dagents", "agt-a")); got != want {
		t.Fatalf("state root=%q want %q", got, want)
	}
	if _, err := WorkspaceStateRoot(workspace, "../other"); err == nil {
		t.Fatal("expected path traversal agent_id to be rejected")
	}

	state, err := EnsureWorkspaceState(workspace, "agt-a")
	if err != nil {
		t.Fatal(err)
	}
	for _, subdir := range []string{"history", "memory"} {
		info, err := os.Stat(filepath.Join(state, subdir))
		if err != nil || !info.IsDir() {
			t.Fatalf("state subdir %q missing: %v", subdir, err)
		}
	}
}

func TestWorkspaceHistoryRelativeRoot_keepsAgentID(t *testing.T) {
	got, err := WorkspaceHistoryRelativeRoot("agt-shared")
	if err != nil {
		t.Fatal(err)
	}
	if got != ".dagents/agt-shared/history" {
		t.Fatalf("history root=%q", got)
	}
	state, err := WorkspaceStateRelativeRoot("agt-shared")
	if err != nil || state != ".dagents/agt-shared" {
		t.Fatalf("state root=%q err=%v", state, err)
	}
}
