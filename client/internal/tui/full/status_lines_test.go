package full

import (
	"strings"
	"testing"
	"time"
)

func TestStatusLineManagerFormatLine(t *testing.T) {
	mgr := newStatusLineManager()
	mgr.Start("prefilling")
	line := mgr.FormatLine("prefilling")
	if !strings.HasPrefix(line, statusTranscriptPrefix) {
		t.Fatalf("prefix = %q", line)
	}
	if !strings.Contains(line, "准备上下文") {
		t.Fatalf("label = %q", line)
	}
	if !strings.Contains(line, "0s") {
		t.Fatalf("elapsed = %q", line)
	}
}

func TestStatusLineManagerKindsOrder(t *testing.T) {
	mgr := newStatusLineManager()
	mgr.Start("thinking")
	mgr.Start("prefilling")
	kinds := mgr.Kinds()
	if len(kinds) != 2 || kinds[0] != "prefilling" || kinds[1] != "thinking" {
		t.Fatalf("kinds = %v", kinds)
	}
}

func TestStatusAnimatedDotsWidth(t *testing.T) {
	for sec := 0; sec < 6; sec++ {
		dots := statusAnimatedDots(sec)
		if len(dots) != 3 {
			t.Fatalf("sec=%d dots=%q len=%d", sec, dots, len(dots))
		}
	}
}

func TestStatusLineManagerActivePhaseLabel(t *testing.T) {
	mgr := newStatusLineManager()
	if mgr.ActivePhaseLabel() != "" {
		t.Fatal("expected empty")
	}
	mgr.Start("thinking")
	if mgr.ActivePhaseLabel() != "思考中" {
		t.Fatalf("got %q", mgr.ActivePhaseLabel())
	}
	mgr.Start("prefilling")
	if mgr.ActivePhaseLabel() != "准备上下文" {
		t.Fatalf("prefilling wins, got %q", mgr.ActivePhaseLabel())
	}
}

func TestStatusLineElapsedIncreases(t *testing.T) {
	mgr := newStatusLineManager()
	mgr.active["prefilling"] = time.Now().Add(-2 * time.Second)
	line := mgr.FormatLine("prefilling")
	if !strings.Contains(line, "2s") {
		t.Fatalf("line = %q", line)
	}
}
