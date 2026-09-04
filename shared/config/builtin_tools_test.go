package config

import "testing"

func TestValidateBuiltinToolNames(t *testing.T) {
	if err := validateBuiltinToolNames([]string{"read_file", "bash_run"}); err != nil {
		t.Fatal(err)
	}
	if err := validateBuiltinToolNames([]string{"not_a_tool"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestExpandBuiltinToolGroups(t *testing.T) {
	got := ExpandBuiltinToolGroups([]string{" hitl ", "hitl", "bash", "memory"})
	want := []string{"ask_user_information", "bash_run", "memory_forget", "memory_get", "memory_search", "remember"}
	if len(got) != len(want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got=%v want=%v", got, want)
		}
	}
}

func TestExpandBuiltinToolGroupsTerminal(t *testing.T) {
	got := ExpandBuiltinToolGroups([]string{"terminal"})
	want := []string{"terminal_command", "terminal_config_list", "terminal_download", "terminal_input", "terminal_list", "terminal_open", "terminal_read", "terminal_terminate", "terminal_upload"}
	if len(got) != len(want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got=%v want=%v", got, want)
		}
	}
}

func TestExpandBuiltinToolGroupsComputer(t *testing.T) {
	got := ExpandBuiltinToolGroups([]string{"computer"})
	want := []string{"computer_use", "screen_capture"}
	if len(got) != len(want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got=%v want=%v", got, want)
		}
	}
}

func TestLinuxToolGroupWasRemoved(t *testing.T) {
	if got := ExpandBuiltinToolGroups([]string{"linux"}); got != nil {
		t.Fatalf("removed linux group should not expand, got %v", got)
	}
	if _, ok := BuiltinToolGroupMembers("linux"); ok {
		t.Fatal("removed linux group should not remain readable")
	}
	for _, name := range AllBuiltinToolGroupNames() {
		if name == "linux" {
			t.Fatal("linux should not be exposed as a tool group")
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
	if members, ok := BuiltinToolGroupMembers("bash"); !ok || len(members) != 1 || members[0] != "bash_run" {
		t.Fatalf("bash members = %v ok=%v want [bash_run]", members, ok)
	}
	if members, ok := BuiltinToolGroupMembers("terminal"); !ok || len(members) != 9 {
		t.Fatalf("terminal members = %v ok=%v want 9 terminal/Linux tools", members, ok)
	}
}
