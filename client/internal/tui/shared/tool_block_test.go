package shared

import "testing"

func TestToolBlockRegistry_expandCollapse(t *testing.T) {
	r := NewToolBlockRegistry()
	r.Register("a")
	r.Register("b")
	if id := r.ExpandLast(); id != "b" {
		t.Fatalf("last should be b, got %q", id)
	}
	if !r.IsExpanded("b", false) {
		t.Fatal("b should be expanded")
	}
	r.CollapseLast()
	if r.IsExpanded("b", false) {
		t.Fatal("b should be collapsed")
	}
}
