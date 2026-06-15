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
