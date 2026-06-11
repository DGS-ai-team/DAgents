package tools

import (
	"context"
	"testing"
)

func TestSetBuiltinEnabledFiltersDefinitions(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	all := len(reg.Definitions())
	if all == 0 {
		t.Fatal("expected builtin tools")
	}
	if err := reg.SetBuiltinEnabled([]string{"read_file", "bash_run", "unknown_tool"}); err == nil {
		t.Fatal("expected unknown tool error")
	}
	if err := reg.SetBuiltinEnabled([]string{"read_file", "bash_run"}); err != nil {
		t.Fatal(err)
	}
	defs := reg.Definitions()
	if len(defs) != 2 {
		t.Fatalf("defs=%d", len(defs))
	}
	for _, d := range defs {
		if d.Function.Name != "read_file" && d.Function.Name != "bash_run" {
			t.Fatalf("unexpected tool %q", d.Function.Name)
		}
	}
	// handlers remain for child delegation
	if _, err := reg.Execute(context.Background(), "read_file", `{"path":"x"}`); err == nil {
		// path missing is ok - not "unknown tool"
	}
	if _, err := reg.Execute(context.Background(), "write_file", `{"path":"a","content":"b"}`); err != nil {
		if err.Error() == "unknown tool: write_file" {
			t.Fatal("write_file handler should still exist")
		}
	}
}

func TestSetBuiltinEnabledEmptyMeansAll(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	all := len(reg.Definitions())
	if err := reg.SetBuiltinEnabled(nil); err != nil {
		t.Fatal(err)
	}
	if len(reg.Definitions()) != all {
		t.Fatalf("want all tools")
	}
}
