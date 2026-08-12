package hooks

import "testing"

func TestDuplicateConfigOrDefault(t *testing.T) {
	if got := DuplicateConfigOrDefault(DuplicateConfig{}); got != DefaultDuplicateConfig() {
		t.Fatalf("zero = %+v", got)
	}
	got := DuplicateConfigOrDefault(DuplicateConfig{Enabled: false, WindowSeconds: 120})
	if got.Enabled || got.WindowSeconds != 120 {
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
