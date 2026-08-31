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

func TestParseSnapshot_workspacePersists(t *testing.T) {
	snap, err := ParseSnapshot([]byte(`{"workspace":{"mode":"custom","path":"C:\\project"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if snap.Workspace.Mode != WorkspaceModeCustom || snap.Workspace.Path != `C:\project` {
		t.Fatalf("workspace=%+v", snap.Workspace)
	}
}
