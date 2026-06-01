package turn

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/hostsnapshot"
	"github.com/DGS-ai-team/DAgents/node/internal/promptcontext"
)

func TestBuildSystemPrompt_includesAgentAndWorkspace(t *testing.T) {
	prompt := BuildSystemPrompt(SystemPromptInput{
		AgentID:   "ops-01",
		FSRoot:    "/data/ws",
		SessionID: "sess-abc",
	})
	if prompt == "" {
		t.Fatal("empty prompt")
	}
	if !containsAll(prompt, "ops-01", "/data/ws", "read_file", "最高优先级规则", "sess-abc", "当前运行环境") {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestBuildSystemPrompt_includesPromptContext(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".runtime")
	dir := filepath.Join(root, "prompt_context")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "user.md"), []byte("prefer concise"), 0o644); err != nil {
		t.Fatal(err)
	}
	hostsnapshot.CaptureAtStartup()
	prompt := BuildSystemPrompt(SystemPromptInput{
		AgentID:   "ops-01",
		FSRoot:    "/data/ws",
		SessionID: "sess-x",
		PromptCtx: promptcontext.NewReader(root),
	})
	if !containsAll(prompt, "用户信息与偏好", "prefer concise", "prompt_context") {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestRunTurnPhase_mapsAwaitingTool(t *testing.T) {
	if got := RunTurnPhase(StateAwaitingTool); got != "awaiting_tool_execution" {
		t.Fatalf("RunTurnPhase = %q", got)
	}
}

func containsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if part == "" {
			continue
		}
		if !contains(text, part) {
			return false
		}
	}
	return true
}

func contains(text, sub string) bool {
	return len(sub) == 0 || (len(text) >= len(sub) && indexOf(text, sub) >= 0)
}

func indexOf(text, sub string) int {
	for i := 0; i+len(sub) <= len(text); i++ {
		if text[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
