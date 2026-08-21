package tools

import (
	"testing"
	"unicode/utf8"
)

func TestOutputBudgetRetainsPrefixAndContinuesDrain(t *testing.T) {
	b := NewOutputBudget(6)
	first := []byte("你好")
	second := []byte(" world")
	if n, err := b.Write(first); err != nil || n != len(first) {
		t.Fatalf("first write = (%d, %v)", n, err)
	}
	if n, err := b.Write(second); err != nil || n != len(second) {
		t.Fatalf("second write = (%d, %v)", n, err)
	}
	if got := b.String(); got != "你好" {
		t.Fatalf("retained output = %q", got)
	}
	if !b.Truncated() {
		t.Fatal("expected truncation marker")
	}
}

func TestOutputBudgetUnlimited(t *testing.T) {
	b := NewOutputBudget(0)
	if _, err := b.Write([]byte("a\x00b")); err != nil {
		t.Fatal(err)
	}
	if b.String() != "a\x00b" || b.Truncated() {
		t.Fatalf("unexpected unlimited budget: %q truncated=%v", b.String(), b.Truncated())
	}
}

func TestOutputBudgetHeadTailRetainsBothEnds(t *testing.T) {
	b := NewHeadTailOutputBudget(10, 4)
	input := []byte("0123456789abcdefghij")
	if n, err := b.Write(input); err != nil || n != len(input) {
		t.Fatalf("write = (%d, %v), want %d bytes consumed", n, err, len(input))
	}
	if got := b.String(); got != "012345\n[... output truncated; showing beginning and end ...]\nghij" {
		t.Fatalf("head/tail output = %q", got)
	}
	if !b.Truncated() {
		t.Fatal("expected truncation marker")
	}
}

func TestOutputBudgetHeadTailIsUTF8Safe(t *testing.T) {
	b := NewHeadTailOutputBudget(8, 4)
	input := []byte("你好世界再见")
	if n, err := b.Write(input); err != nil || n != len(input) {
		t.Fatalf("write = (%d, %v)", n, err)
	}
	if !utf8.ValidString(b.String()) {
		t.Fatalf("output is not valid UTF-8: %q", b.String())
	}
}
