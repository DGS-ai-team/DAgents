package promptcontext

import "testing"

func TestContentReaderIgnoresDisk(t *testing.T) {
	r := NewContentReader(Content{
		Soul:     "from-db",
		User:     "user-db",
		Custom:   "custom-db",
		LongTerm: "lt-db",
	})
	if got := r.ReadSoul(); got != "from-db" {
		t.Fatalf("soul = %q", got)
	}
	if got := r.ReadLongTermMemory(); got != "lt-db" {
		t.Fatalf("long_term = %q", got)
	}
	off := false
	r.SetFilter(Filter{SoulEnabled: &off})
	if got := r.ReadSoul(); got != "" {
		t.Fatalf("disabled soul = %q", got)
	}
}
