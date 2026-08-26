package turn

import (
	"context"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tokens"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

type stubBashExecutor struct {
	output string
}

func (s stubBashExecutor) Definitions() []tools.ToolDef { return nil }

func (s stubBashExecutor) Execute(_ context.Context, name, _ string) (string, error) {
	if name != "bash_run" {
		return "", nil
	}
	return s.output, nil
}

func (s stubBashExecutor) StartBackground(context.Context, string, string, string, string) (string, error) {
	return "", nil
}

func (s stubBashExecutor) TakeBashCompressStatsForCall(string) map[string]any { return nil }
func (s stubBashExecutor) TakeToolResultMediaForCall(string) map[string]any   { return nil }
func (s stubBashExecutor) TakeReadImageVisionForCall(string) *tools.ReadImageVisionPayload {
	return nil
}

func TestExecuteTool_bashHistorySpillsButSSEFull(t *testing.T) {
	root := t.TempDir()
	hub := stream.NewHub(8, logx.Discard())
	long := strings.Repeat("o", 50000)
	orch := NewOrchestrator(
		"a1", root, hub, &llm.MockClient{},
		stubBashExecutor{output: long},
		nil, SkillAccess{}, DefaultMaxToolLoops(), nil, nil,
		hooks.RuntimeConfig{
			Duplicate:  hooks.DefaultDuplicateConfig(),
			ToolResult: hooks.DefaultToolResultConfig(root),
		},
		logx.Discard(),
	)
	tc := llm.ToolCall{
		ID:   "call-long",
		Type: "function",
		Function: llm.ToolCallFunction{
			Name:      "bash_run",
			Arguments: `{"call_purpose":"test","command":"echo x"}`,
		},
	}
	history := []llm.Message{
		{Role: "user", Content: "run"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{tc}},
	}
	if err := orch.executeTool(context.Background(), "sess-spill", &history, tc, nil); err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 || history[2].Role != "tool" {
		t.Fatalf("history len=%d last=%+v", len(history), history[len(history)-1])
	}
	if history[2].Content == long {
		t.Fatal("history should be summarized")
	}
	if !strings.Contains(history[2].Content, "tokens") {
		t.Fatalf("missing token hint: %q", history[2].Content)
	}
	if tokens.Estimate(history[2].Content) > 13000 {
		t.Fatalf("history token estimate too high: %v", tokens.Estimate(history[2].Content))
	}
	forClient, _, _ := orch.splitToolResult("sess-spill", tc, long)
	if forClient != long {
		t.Fatalf("client content len=%d want %d", len(forClient), len(long))
	}
}

func TestExecuteTool_readFileNotSpilled(t *testing.T) {
	root := t.TempDir()
	reg, err := tools.NewRegistry(root, 30)
	if err != nil {
		t.Fatal(err)
	}
	hub := stream.NewHub(8, logx.Discard())
	orch := NewOrchestrator(
		"a1", root, hub, &llm.MockClient{}, reg, nil, SkillAccess{}, DefaultMaxToolLoops(), nil, nil,
		hooks.RuntimeConfig{
			Duplicate:  hooks.DefaultDuplicateConfig(),
			ToolResult: hooks.DefaultToolResultConfig(root),
		},
		logx.Discard(),
	)
	tc := llm.ToolCall{
		ID: "c1", Type: "function",
		Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"call_purpose":"t","path":"missing.txt"}`},
	}
	history := []llm.Message{{Role: "assistant", ToolCalls: []llm.ToolCall{tc}}}
	if err := orch.executeTool(context.Background(), "s1", &history, tc, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(history[len(history)-1].Content, "已省略") {
		t.Fatal("read_file should not use bash spill hint")
	}
}
