package tools

import (
	"strings"
	"testing"
)

func TestBashRunToolDescription_windows(t *testing.T) {
	desc := bashRunToolDescription(true)
	cmdDesc := bashRunCommandParamDescription(true)
	shellDesc := bashRunShellTypeParamDescription(true)

	for _, sub := range []string{"PowerShell", "Windows", "powershell", "terminal_open"} {
		if !strings.Contains(desc, sub) {
			t.Fatalf("desc missing %q: %q", sub, desc)
		}
	}
	if strings.Contains(desc, "bash/cmd/powershell") {
		t.Fatalf("windows desc should not list generic shells: %q", desc)
	}
	if !strings.Contains(cmdDesc, "PowerShell") {
		t.Fatalf("cmdDesc = %q", cmdDesc)
	}
	if !strings.Contains(shellDesc, "powershell") {
		t.Fatalf("shellDesc = %q", shellDesc)
	}
}

func TestBashRunToolDescription_unix(t *testing.T) {
	desc := bashRunToolDescription(false)
	cmdDesc := bashRunCommandParamDescription(false)
	shellDesc := bashRunShellTypeParamDescription(false)

	for _, sub := range []string{"bash", "terminal_open"} {
		if !strings.Contains(desc, sub) {
			t.Fatalf("desc missing %q: %q", sub, desc)
		}
	}
	if strings.Contains(desc, "bash/cmd/powershell") || strings.Contains(desc, "执行 PowerShell") {
		t.Fatalf("unix desc should emphasize bash only: %q", desc)
	}
	if !strings.Contains(cmdDesc, "bash") {
		t.Fatalf("cmdDesc = %q", cmdDesc)
	}
	if !strings.Contains(shellDesc, "bash") {
		t.Fatalf("shellDesc = %q", shellDesc)
	}
}

func TestBashRunToolDef_usesHostSnapshot(t *testing.T) {
	def := bashRunToolDef()
	if def.Function.Name != "bash_run" || def.Function.Description == "" {
		t.Fatalf("def = %+v", def)
	}
	props := def.Function.Parameters["properties"].(map[string]any)
	cmd := props["command"].(map[string]any)["description"].(string)
	if cmd == "" {
		t.Fatal("empty command description")
	}
}
