package nodectl

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveLayout_fromBinDir(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(cfg, []byte("listen:\n  port: 18765\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeExe := filepath.Join(binDir, "dagents-tray.exe")
	if err := os.WriteFile(fakeExe, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv(EnvHome, "")
	// ResolveLayout uses os.Executable; stub by setting DAGENTS_HOME instead.
	t.Setenv(EnvHome, root)
	layout, err := ResolveLayout("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if layout.Home != root {
		t.Fatalf("home = %q, want %q", layout.Home, root)
	}
	if layout.ConfigPath != cfg {
		t.Fatalf("config = %q, want %q", layout.ConfigPath, cfg)
	}
	wantNode := filepath.Join(root, "bin", "dagents-node.exe")
	if layout.NodeExe != wantNode {
		t.Fatalf("node exe = %q, want %q", layout.NodeExe, wantNode)
	}
}
