package promptcontext

import "testing"

func TestContentReaderIgnoresDisk(t *testing.T) {
	r := NewContentReader(Content{
		Soul:   "from-db",
		Custom: "custom-db",
	})
	if got := r.ReadSoul(); got != "from-db" {
		t.Fatalf("soul = %q", got)
	}
	off := false
	r.SetFilter(Filter{SoulEnabled: &off})
	if got := r.ReadSoul(); got != "" {
		t.Fatalf("disabled soul = %q", got)
	}
}
