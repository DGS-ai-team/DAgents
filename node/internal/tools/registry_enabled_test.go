package tools

import (
	"context"
	"strings"
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
	// P0：未启用工具 Execute soft reject（handler 仍保留，供子 Agent bypass）
	if _, err := reg.Execute(context.Background(), "write_file", `{"path":"a","content":"b"}`); err == nil {
		t.Fatal("expected write_file soft reject when disabled")
	} else if !strings.Contains(err.Error(), "is not enabled") {
		t.Fatalf("want not-enabled error, got %v", err)
	}
	// bypass 后仍可执行（子 Agent 路径）
	if _, err := reg.Execute(WithEnabledBypass(context.Background()), "write_file", `{"path":"a","content":"b"}`); err != nil {
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

func TestRetiredGoalCheckpointIsNotRegisteredOrCallable(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	for _, def := range reg.Definitions() {
		if def.Function.Name == "goal_checkpoint" {
			t.Fatal("retired goal_checkpoint must not be exposed to the model")
		}
	}
	if _, err := reg.Execute(context.Background(), "goal_checkpoint", `{}`); err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("retired goal_checkpoint unexpectedly callable: %v", err)
	}
	if err := reg.SetBuiltinEnabled([]string{"goal_checkpoint"}); err == nil || !strings.Contains(err.Error(), "unknown builtin tool") {
		t.Fatalf("retired goal_checkpoint unexpectedly accepted by allowlist: %v", err)
	}
}

func TestSetMultimodalEnabledFiltersReadImage(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range reg.Definitions() {
		if d.Function.Name == "read_image" {
			t.Fatal("read_image should be hidden by default")
		}
	}
	reg.SetMultimodalEnabled(true)
	found := false
	for _, d := range reg.Definitions() {
		if d.Function.Name == "read_image" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected read_image when multimodal enabled")
	}
	out, err := reg.Execute(context.Background(), "read_image", `{"path":"missing.png"}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "多模态未启用") {
		t.Fatalf("unexpected disabled message when enabled: %q", out)
	}
	reg.SetMultimodalEnabled(false)
	out, err = reg.Execute(context.Background(), "read_image", `{"path":"missing.png"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "多模态未启用") {
		t.Fatalf("want disabled message, got %q", out)
	}
}
