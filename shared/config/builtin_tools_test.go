package config

import (
	"strings"
	"testing"
)

func TestValidateBuiltinToolNames(t *testing.T) {
	if err := validateBuiltinToolNames([]string{"read_file", "bash_run"}); err != nil {
		t.Fatal(err)
	}
	if err := validateBuiltinToolNames([]string{"not_a_tool"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadFile_rejectsUnknownBuiltinToolGroup(t *testing.T) {
	path, _ := testConfigPath(t, `
agent_id: test-agent
tools:
  enabled_groups:
    - fs
    - fake_group
`)
	_, err := LoadFile(path)
	if err == nil || !strings.Contains(err.Error(), "fake_group") {
		t.Fatalf("LoadFile err = %v", err)
	}
}

func TestLoadFile_rejectsDeprecatedToolsEnabled(t *testing.T) {
	path, _ := testConfigPath(t, `
agent_id: test-agent
tools:
  enabled:
    - read_file
`)
	_, err := LoadFile(path)
	if err == nil || !strings.Contains(err.Error(), "enabled_groups") {
		t.Fatalf("LoadFile err = %v", err)
	}
}

func TestNormalizedBuiltinEnabledGroupsExpands(t *testing.T) {
	cfg := &Config{Tools: ToolsConfig{EnabledGroups: []string{" hitl ", "hitl", "bash"}}}
	got := cfg.Tools.NormalizedBuiltinEnabled()
	want := []string{"ask_user_information", "background_job_cancel", "background_job_status", "bash_run"}
	if len(got) != len(want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got=%v want=%v", got, want)
		}
	}
}

func TestNormalizedBuiltinEnabledEmptyMeansAll(t *testing.T) {
	cfg := &Config{Tools: ToolsConfig{}}
	if got := cfg.Tools.NormalizedBuiltinEnabled(); got != nil {
		t.Fatalf("got=%v want nil", got)
	}
}

func TestAllToolsAssignedToExactlyOneGroup(t *testing.T) {
	seen := make(map[string]string)
	for group, tools := range builtinToolGroups {
		for _, tool := range tools {
			if prev, ok := seen[tool]; ok {
				t.Fatalf("tool %q in groups %q and %q", tool, prev, group)
			}
			seen[tool] = group
		}
	}
	for name := range knownBuiltinTools {
		if _, ok := seen[name]; !ok {
			t.Fatalf("tool %q not in any group", name)
		}
	}
}

func TestBuiltinToolGroupMembers(t *testing.T) {
	members, ok := BuiltinToolGroupMembers("fs")
	if !ok || len(members) != 6 {
		t.Fatalf("fs members = %v ok=%v", members, ok)
	}
	if _, ok := BuiltinToolGroupMembers("nope"); ok {
		t.Fatal("expected false for unknown group")
	}
}
