package uifocus

import (
	"testing"
	"time"
)

func TestStoreReportAndFocus(t *testing.T) {
	s := NewStore()
	s.Report("sess-a", 2*time.Second)
	if !s.IsFocused("sess-a") {
		t.Fatal("expected focused")
	}
	if s.IsFocused("sess-b") {
		t.Fatal("other session must not be focused")
	}
	s.Report("", 0)
	if s.IsFocused("sess-a") {
		t.Fatal("expected cleared")
	}
}

func TestStoreExpires(t *testing.T) {
	s := NewStore()
	s.Report("sess-a", 20*time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	if s.IsFocused("sess-a") {
		t.Fatal("expected expired")
	}
}
