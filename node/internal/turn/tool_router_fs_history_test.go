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

type stubReadFileExecutor struct {
	output string
}

func (s stubReadFileExecutor) Definitions() []tools.ToolDef { return nil }

func (s stubReadFileExecutor) Execute(_ context.Context, name, _ string) (string, error) {
	if name != "read_file" {
		return "", nil
	}
	return s.output, nil
}

func (s stubReadFileExecutor) TakeBashCompressStatsForCall(string) map[string]any { return nil }
func (s stubReadFileExecutor) TakeToolResultMediaForCall(string) map[string]any   { return nil }
func (s stubReadFileExecutor) TakeReadImageVisionForCall(string) *tools.ReadImageVisionPayload {
	return nil
}

func TestExecuteTool_readFileHistorySpillsButSSEFull(t *testing.T) {
	root := t.TempDir()
	hub := stream.NewHub(8, logx.Discard())
	long := strings.Repeat("o", 50000)
	orch := NewOrchestrator(
		"a1", root, hub, &llm.MockClient{},
		stubReadFileExecutor{output: long},
		nil, SkillAccess{}, nil, nil,
		hooks.RuntimeConfig{
			Duplicate:  hooks.DefaultDuplicateConfig(),
			ToolResult: hooks.DefaultToolResultConfig(root),
		},
		logx.Discard(),
	)
	tc := llm.ToolCall{
		ID:   "call-read-long",
		Type: "function",
		Function: llm.ToolCallFunction{
			Name:      "read_file",
			Arguments: `{"call_purpose":"test","path":"big.txt","line_offset":1,"line_limit":100}`,
		},
	}
	history := []llm.Message{
		{Role: "user", Content: "read"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{tc}},
	}
	if err := orch.executeTool(context.Background(), "sess-fs", &history, tc, nil); err != nil {
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
	forClient, _, _ := orch.splitToolResult("sess-fs", tc, long)
	if forClient != long {
		t.Fatalf("client content len=%d want %d", len(forClient), len(long))
	}
}

func TestExecuteTool_writeFileNotSpilledByHook(t *testing.T) {
	root := t.TempDir()
	hub := stream.NewHub(8, logx.Discard())
	long := strings.Repeat("z", 50000)
	orch := NewOrchestrator(
		"a1", root, hub, &llm.MockClient{},
		stubToolOutputExecutor{toolName: "write_file", output: long},
		nil, SkillAccess{}, nil, nil,
		hooks.RuntimeConfig{
			Duplicate:  hooks.DefaultDuplicateConfig(),
			ToolResult: hooks.DefaultToolResultConfig(root),
		},
		logx.Discard(),
	)
	tc := llm.ToolCall{
		ID: "c-w", Type: "function",
		Function: llm.ToolCallFunction{Name: "write_file", Arguments: `{"call_purpose":"t","path":"out.txt","content":"hi"}`},
	}
	history := []llm.Message{{Role: "assistant", ToolCalls: []llm.ToolCall{tc}}}
	if err := orch.executeTool(context.Background(), "s-w", &history, tc, nil); err != nil {
		t.Fatal(err)
	}
	if history[len(history)-1].Content != long {
		t.Fatalf("write_file passthrough body len=%d", len(history[len(history)-1].Content))
	}
	if strings.Contains(history[len(history)-1].Content, "tokens") {
		t.Fatal("write_file should not use tool_result spill")
	}
}

func TestExecuteTool_searchReplaceHistorySpillsButSSEFull(t *testing.T) {
	root := t.TempDir()
	hub := stream.NewHub(8, logx.Discard())
	long := strings.Repeat("o", 50000)
	orch := NewOrchestrator(
		"a1", root, hub, &llm.MockClient{},
		stubToolOutputExecutor{toolName: "search_replace", output: long},
		nil, SkillAccess{}, nil, nil,
		hooks.RuntimeConfig{
			Duplicate:  hooks.DefaultDuplicateConfig(),
			ToolResult: hooks.DefaultToolResultConfig(root),
		},
		logx.Discard(),
	)
	tc := llm.ToolCall{
		ID: "c-sr", Type: "function",
		Function: llm.ToolCallFunction{Name: "search_replace", Arguments: `{"call_purpose":"t","path":"a.txt","old_string":"x","new_string":"y"}`},
	}
	history := []llm.Message{{Role: "assistant", ToolCalls: []llm.ToolCall{tc}}}
	if err := orch.executeTool(context.Background(), "s-sr", &history, tc, nil); err != nil {
		t.Fatal(err)
	}
	if history[len(history)-1].Content == long {
		t.Fatal("history should be summarized")
	}
	if !strings.Contains(history[len(history)-1].Content, "tokens") {
		t.Fatalf("missing token hint: %q", history[len(history)-1].Content)
	}
	forClient, _, _ := orch.splitToolResult("s-sr", tc, long)
	if forClient != long {
		t.Fatalf("client content len=%d want %d", len(forClient), len(long))
	}
}

func TestExecuteTool_globFilesHistorySpillsButSSEFull(t *testing.T) {
	root := t.TempDir()
	hub := stream.NewHub(8, logx.Discard())
	long := strings.Repeat("p", 50000)
	orch := NewOrchestrator(
		"a1", root, hub, &llm.MockClient{},
		stubToolOutputExecutor{toolName: "glob_files", output: long},
		nil, SkillAccess{}, nil, nil,
		hooks.RuntimeConfig{
			Duplicate:  hooks.DefaultDuplicateConfig(),
			ToolResult: hooks.DefaultToolResultConfig(root),
		},
		logx.Discard(),
	)
	tc := llm.ToolCall{
		ID: "c-gl", Type: "function",
		Function: llm.ToolCallFunction{Name: "glob_files", Arguments: `{"call_purpose":"t","directory":".","glob_pattern":"**/*"}`},
	}
	history := []llm.Message{{Role: "assistant", ToolCalls: []llm.ToolCall{tc}}}
	if err := orch.executeTool(context.Background(), "s-gl", &history, tc, nil); err != nil {
		t.Fatal(err)
	}
	if history[len(history)-1].Content == long {
		t.Fatal("history should be summarized")
	}
	if !strings.Contains(history[len(history)-1].Content, "tokens") {
		t.Fatalf("missing token hint: %q", history[len(history)-1].Content)
	}
}

func TestExecuteTool_bashRunHistorySpillsButSSEFull(t *testing.T) {
	root := t.TempDir()
	hub := stream.NewHub(8, logx.Discard())
	long := strings.Repeat("a", 50000)
	orch := NewOrchestrator(
		"a1", root, hub, &llm.MockClient{},
		stubToolOutputExecutor{toolName: "bash_run", output: long},
		nil, SkillAccess{}, nil, nil,
		hooks.RuntimeConfig{
			Duplicate:  hooks.DefaultDuplicateConfig(),
			ToolResult: hooks.DefaultToolResultConfig(root),
		},
		logx.Discard(),
	)
	tc := llm.ToolCall{
		ID: "c-br", Type: "function",
		Function: llm.ToolCallFunction{Name: "bash_run", Arguments: `{"call_purpose":"t","command":"echo hi"}`},
	}
	history := []llm.Message{{Role: "assistant", ToolCalls: []llm.ToolCall{tc}}}
	if err := orch.executeTool(context.Background(), "s-ai", &history, tc, nil); err != nil {
		t.Fatal(err)
	}
	if history[len(history)-1].Content == long {
		t.Fatal("history should be summarized")
	}
	if !strings.Contains(history[len(history)-1].Content, "tokens") {
		t.Fatalf("missing token hint: %q", history[len(history)-1].Content)
	}
	forClient, _, _ := orch.splitToolResult("s-ai", tc, long)
	if forClient != long {
		t.Fatalf("client content len=%d want %d", len(forClient), len(long))
	}
}

type stubToolOutputExecutor struct {
	toolName string
	output   string
}

func (s stubToolOutputExecutor) Definitions() []tools.ToolDef { return nil }

func (s stubToolOutputExecutor) Execute(_ context.Context, name, _ string) (string, error) {
	if name == s.toolName {
		return s.output, nil
	}
	return "", nil
}

func (s stubToolOutputExecutor) TakeBashCompressStatsForCall(string) map[string]any { return nil }
func (s stubToolOutputExecutor) TakeToolResultMediaForCall(string) map[string]any   { return nil }
func (s stubToolOutputExecutor) TakeReadImageVisionForCall(string) *tools.ReadImageVisionPayload {
	return nil
}
