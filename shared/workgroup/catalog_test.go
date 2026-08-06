package membertools

import (
	"testing"
)

func TestCatalogDefaultsAreSubsetOfExecutable(t *testing.T) {
	exec := map[string]struct{}{}
	for _, n := range ExecutableToolNames() {
		exec[n] = struct{}{}
	}
	if len(exec) < 8 {
		t.Fatalf("expected full fs+bash catalog, got %d", len(exec))
	}
	for _, n := range DefaultAllowToolNames() {
		if _, ok := exec[n]; !ok {
			t.Fatalf("default %q not in executable set", n)
		}
	}
	if _, ok := exec["bash_run"]; !ok {
		t.Fatal("missing bash_run")
	}
	for _, n := range DefaultAllowToolNames() {
		if n == "bash_run" {
			t.Fatal("bash_run must not be default-selected")
		}
	}
	se := SideEffectClasses()
	if se["read_file"] != "fs_read" || se["bash_run"] != "shell" {
		t.Fatalf("side effects: %#v", se)
	}
	schemas := ToolSchemas()
	if schemas["read_file"] == nil || schemas["bash_run"] == nil {
		t.Fatal("missing schemas")
	}
}
