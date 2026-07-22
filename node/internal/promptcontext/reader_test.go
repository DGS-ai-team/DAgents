package promptcontext_test

import (
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/promptcontext"
)

func TestReaderSidecarAndCustom(t *testing.T) {
	r := promptcontext.NewContentReader(promptcontext.Content{
		Soul:   "agent soul",
		Custom: "do X",
	})

	stable := r.BuildStableContextSections()
	if !strings.Contains(stable, "以下是你的设定") || !strings.Contains(stable, "agent soul") {
		t.Fatalf("stable = %q", stable)
	}
	custom := r.BuildCustomSection()
	if !strings.Contains(custom, "临时/专项指令") || !strings.Contains(custom, "do X") {
		t.Fatalf("custom = %q", custom)
	}
}

func TestReaderFilterDisablesLongTerm(t *testing.T) {
	r := promptcontext.NewContentReader(promptcontext.Content{LongTerm: "remember me"})
	off := false
	r.SetFilter(promptcontext.Filter{LongTermEnabled: &off})
	if got := r.ReadLongTermMemory(); got != "" {
		t.Fatalf("expected empty when disabled, got %q", got)
	}
	on := true
	r.SetFilter(promptcontext.Filter{LongTermEnabled: &on})
	if got := r.ReadLongTermMemory(); got != "remember me" {
		t.Fatalf("got %q", got)
	}
}

func TestUpdateLongTerm(t *testing.T) {
	r := promptcontext.NewContentReader(promptcontext.Content{})
	r.UpdateLongTerm("new memory")
	if got := r.ReadLongTermMemory(); got != "new memory" {
		t.Fatalf("got %q", got)
	}
}
