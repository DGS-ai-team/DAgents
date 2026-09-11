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
	r.SetPreferredName("阿强")

	stable := r.BuildStableContextSections()
	if !strings.Contains(stable, "以下是你的设定") || !strings.Contains(stable, "agent soul") {
		t.Fatalf("stable = %q", stable)
	}
	if !strings.Contains(stable, "请称呼用户为：阿强") {
		t.Fatalf("expected preferred name, got %q", stable)
	}
	custom := r.BuildCustomSection()
	if !strings.Contains(custom, "临时/专项指令") || !strings.Contains(custom, "do X") {
		t.Fatalf("custom = %q", custom)
	}
}

func TestReaderUsesPreferredNameForUserInfo(t *testing.T) {
	r := promptcontext.NewContentReader(promptcontext.Content{})
	r.SetPreferredName("小明")
	stable := r.BuildStableContextSections()
	if !strings.Contains(stable, "用户信息") || !strings.Contains(stable, "小明") {
		t.Fatalf("preferred name should provide user info, got %q", stable)
	}
}
