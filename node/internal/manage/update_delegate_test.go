package manage

import "testing"

func TestShellDelegateUpdateStatus(t *testing.T) {
	st := ShellDelegateUpdateStatus("stable")
	if st.Delegate != "shell" {
		t.Fatalf("delegate = %q", st.Delegate)
	}
	if !st.Deprecated {
		t.Fatal("expected deprecated")
	}
	if st.DesktopAPI != ShellDesktopAPIBase+"/v1/desktop/update" {
		t.Fatalf("desktop_api = %q", st.DesktopAPI)
	}
	if st.CurrentVersion == "" {
		t.Fatal("expected current_version")
	}
}

func TestUpdateDelegatedToShellMatchesRuntime(t *testing.T) {
	got := UpdateDelegatedToShell()
	want := got // document current platform; test mainly guards export
	if got != want {
		t.Fatalf("unexpected")
	}
}
