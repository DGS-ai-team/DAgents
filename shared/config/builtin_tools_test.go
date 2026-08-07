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

func TestLoadFile_ignoresLegacyNodeEnabledGroups(t *testing.T) {
	path, _ := testConfigPath(t, `
node_id: test-agent
tools:
  enabled_groups:
    - fs
    - fake_group
`)
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile err = %v", err)
	}
	if cfg == nil {
		t.Fatal("nil config")
	}
}

func TestLoadFile_rejectsDeprecatedToolsEnabled(t *testing.T) {
	path, _ := testConfigPath(t, `
node_id: test-agent
tools:
  enabled:
    - read_file
`)
	_, err := LoadFile(path)
	if err == nil || !strings.Contains(err.Error(), "tools.enabled") {
		t.Fatalf("LoadFile err = %v", err)
	}
}

func TestExpandBuiltinToolGroups(t *testing.T) {
	got := ExpandBuiltinToolGroups([]string{" hitl ", "hitl", "bash", "memory"})
	want := []string{"ask_user_information", "background_job_cancel", "background_job_status", "bash_run", "remember"}
	if len(got) != len(want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got=%v want=%v", got, want)
		}
	}
}

func TestExpandBuiltinToolGroupsBrowser(t *testing.T) {
	got := ExpandBuiltinToolGroups([]string{"browser"})
	want := []string{"browser_run_task", "browser_task_cancel", "browser_task_status"}
	if len(got) != len(want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got=%v want=%v", got, want)
		}
	}
	ops := ExpandBuiltinToolGroups([]string{"browser_ops"})
	if ops != nil {
		t.Fatalf("retired browser_ops should expand to nil, got %v", ops)
	}
	if err := ValidateBuiltinToolGroups([]string{"browser_ops"}); err == nil {
		t.Fatal("expected error for retired browser_ops")
	}
}

func TestExpandBuiltinToolGroupsEmpty(t *testing.T) {
	if got := ExpandBuiltinToolGroups(nil); got != nil {
		t.Fatalf("got=%v want nil", got)
	}
}

func TestValidateBuiltinToolGroups(t *testing.T) {
	if err := ValidateBuiltinToolGroups([]string{"fs"}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateBuiltinToolGroups([]string{"fake_group"}); err == nil {
		t.Fatal("expected error")
	}
	if err := ValidateBuiltinToolGroups([]string{"a2a"}); err == nil {
		t.Fatal("expected error for retired a2a")
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
	want := []string{
		"read_file", "show_image", "read_image", "write_file",
		"glob_files", "grep_file", "grep_files", "search_replace",
	}
	if !ok || len(members) != len(want) {
		t.Fatalf("fs members = %v ok=%v want len %d", members, ok, len(want))
	}
	for i := range want {
		if members[i] != want[i] {
			t.Fatalf("fs members[%d] = %q want %q (full=%v)", i, members[i], want[i], members)
		}
	}
	if _, ok := BuiltinToolGroupMembers("nope"); ok {
		t.Fatal("expected false for unknown group")
	}
}

func TestLoadFile_ignoresRetiredA2AGroupInLegacyNodeConfig(t *testing.T) {
	path, _ := testConfigPath(t, `
node_id: test-agent
tools:
  enabled_groups:
    - a2a
`)
	if _, err := LoadFile(path); err != nil {
		t.Fatalf("legacy node enabled_groups must be ignored: %v", err)
	}
}
