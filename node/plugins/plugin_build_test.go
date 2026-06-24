//go:build linux

package plugins_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"plugin"
	"runtime"
	"testing"
)

func TestProtectLoadedSkillPluginBuilds(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("plugin build test requires linux")
	}
	nodeDir := repoNodeDir(t)
	out := filepath.Join(t.TempDir(), "protect-loaded-skill.so")
	cmd := exec.Command("go", "build", "-buildmode=plugin", "-o", out, "./plugins/protect-loaded-skill")
	cmd.Dir = nodeDir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=1")
	outBytes, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("plugin build failed: %v\n%s", err, outBytes)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Fatal("plugin output is empty")
	}

	p, err := plugin.Open(out)
	if err != nil {
		t.Fatalf("plugin.Open: %v", err)
	}
	if _, err := p.Lookup("Register"); err != nil {
		t.Fatalf("plugin.Lookup Register: %v", err)
	}
}

func repoNodeDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), ".."))
}
