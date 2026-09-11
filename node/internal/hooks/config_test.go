package hooks

import "testing"

func TestDuplicateConfigOrDefault(t *testing.T) {
	if got := DuplicateConfigOrDefault(DuplicateConfig{}); !got.IsEnabled() || got.WindowSeconds != 60 {
		t.Fatalf("zero = %+v", got)
	}
	disabled := false
	got := DuplicateConfigOrDefault(DuplicateConfig{Enabled: &disabled, WindowSeconds: 120})
	if got.IsEnabled() || got.WindowSeconds != 120 {
		t.Fatalf("override = %+v", got)
	}
}

func TestInjectTodayDateConfigOrDefault(t *testing.T) {
	if !InjectTodayDateConfigOrDefault(InjectTodayDateConfig{}).IsEnabled() {
		t.Fatal("nil enabled should default true")
	}
	off := false
	if InjectTodayDateConfigOrDefault(InjectTodayDateConfig{Enabled: &off}).IsEnabled() {
		t.Fatal("expected disabled")
	}
}

func TestToolResultConfigOrDefault_preservesExplicitDisabled(t *testing.T) {
	disabled := false
	got := ToolResultConfigOrDefault(ToolResultConfig{Enabled: &disabled})
	if got.IsEnabled() {
		t.Fatal("explicit disabled configuration was replaced by the default")
	}
	if got.SpillThresholdTokens <= 0 || len(got.Tools) == 0 {
		t.Fatalf("expected the remaining defaults, got %+v", got)
	}
}

func TestToolResultConfigOrDefault_zeroUsesDefaults(t *testing.T) {
	got := ToolResultConfigOrDefault(ToolResultConfig{})
	if !got.IsEnabled() {
		t.Fatal("zero configuration should use the enabled default")
	}
}
