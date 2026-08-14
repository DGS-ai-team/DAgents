package uifocus

import (
	"testing"
	"time"
)

func TestStoreReportAndFocus(t *testing.T) {
	s := NewStore()
	s.Report("tab-a", "sess-a", 2*time.Second)
	if !s.IsFocused("sess-a") {
		t.Fatal("expected focused")
	}
	if s.IsFocused("sess-b") {
		t.Fatal("other session must not be focused")
	}
	s.Report("tab-a", "", 0)
	if s.IsFocused("sess-a") {
		t.Fatal("expected cleared")
	}
}

func TestStoreExpires(t *testing.T) {
	s := NewStore()
	s.Report("tab-a", "sess-a", 20*time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	if s.IsFocused("sess-a") {
		t.Fatal("expected expired")
	}
}

func TestStoreSourceScopedClear(t *testing.T) {
	s := NewStore()
	s.Report("tab-a", "sess-a", DefaultTTL)
	s.Report("tab-b", "sess-a", DefaultTTL)
	s.Report("tab-a", "", DefaultTTL)
	if !s.IsFocused("sess-a") {
		t.Fatal("clearing one source must retain another source")
	}
	s.Report("tab-b", "", DefaultTTL)
	if s.IsFocused("sess-a") {
		t.Fatal("expected all source claims cleared")
	}
}
