package turn

import (
	"context"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

func TestTurnContextMetrics_toolCallAndDoneSSE(t *testing.T) {
	hub := stream.NewHub(16, logx.Discard())
	orch := NewOrchestrator(
		"a1", t.TempDir(), hub, &llm.MockClient{},
		stubStatusPollExecutor{},
		nil, SkillAccess{}, nil, nil,
		hooks.RuntimeConfig{
			Duplicate:  hooks.DefaultDuplicateConfig(),
			ToolResult: hooks.DefaultToolResultConfig(t.TempDir()),
		},
		logx.Discard(),
	)
	orch.resetContextMetrics("sess-m")
	tcStatus := llm.ToolCall{
		ID: "call-read", Type: "function",
		Function: llm.ToolCallFunction{
			Name:      "read_file",
			Arguments: `{"call_purpose":"read","path":"a.txt"}`,
		},
	}
	history := []llm.Message{
		{Role: "user", Content: "check"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{tcStatus}},
	}
	ch := hub.Subscribe(0)
	defer hub.Unsubscribe(ch)
	if err := orch.executeTool(context.Background(), "sess-m", &history, tcStatus, nil); err != nil {
		t.Fatal(err)
	}
	m := orch.contextMetrics("sess-m")
	if m == nil || m.ToolCalls != 1 {
		t.Fatalf("metrics after executeTool = %+v", m)
	}
	orch.publishTurnFinished("sess-m", "stop")
	var finishedPayload map[string]any
	for i := 0; i < 5; i++ {
		ev := <-ch
		if ev.Type == "turn_finished" {
			finishedPayload = ev.Data
			break
		}
	}
	if finishedPayload == nil {
		t.Fatal("missing turn_finished event")
	}
	raw, ok := finishedPayload["tool_context_metrics"].(map[string]any)
	if !ok {
		t.Fatalf("missing tool_context_metrics: %+v", finishedPayload)
	}
	if n, ok := raw["tool_calls"].(int); !ok || n != 1 {
		if f, ok := raw["tool_calls"].(float64); !ok || int(f) != 1 {
			t.Fatalf("tool_calls = %v (%T)", raw["tool_calls"], raw["tool_calls"])
		}
	}
}

func TestTurnContextMetrics_readFileEncodingAndRepeat(t *testing.T) {
	orch := NewOrchestrator("a1", t.TempDir(), stream.NewHub(4, logx.Discard()), &llm.MockClient{}, nil, nil, SkillAccess{}, nil, nil, hooks.RuntimeConfig{Duplicate: hooks.DefaultDuplicateConfig()}, logx.Discard())
	orch.resetContextMetrics("sess-r")
	content := strings.Join([]string{
		"文件编码: gbk",
		"编码来源: 检测",
		"编码提示: 正文疑似乱码",
		"---",
		"hello",
	}, "\n")
	args := `{"path":"a.txt","call_purpose":"t"}`
	orch.recordToolCall("sess-r", "read_file")
	orch.recordToolResult("sess-r", "read_file", args, content, "", false)
	orch.recordToolCall("sess-r", "read_file")
	orch.recordToolResult("sess-r", "read_file", args, strings.Replace(content, "检测", "缓存", 1), "", false)
	m := orch.contextMetrics("sess-r")
	if m.ReadFileCalls != 2 || m.ReadFilePathRepeats != 1 {
		t.Fatalf("repeat metrics = %+v", m)
	}
	if m.EncodingSourceDetect != 1 || m.EncodingSourceCache != 1 || m.EncodingGarbledHints != 2 {
		t.Fatalf("encoding metrics = %+v", m)
	}
}

func TestTurnContextMetrics_snapshotToolLoops(t *testing.T) {
	orch := NewOrchestrator("a1", t.TempDir(), stream.NewHub(4, logx.Discard()), &llm.MockClient{}, nil, nil, SkillAccess{}, nil, nil, hooks.RuntimeConfig{Duplicate: hooks.DefaultDuplicateConfig()}, logx.Discard())
	orch.resetContextMetrics("s")
	orch.recordToolLoop("s", 3)
	snap := orch.contextMetrics("s").snapshot()
	if snap["tool_loops"] != 3 {
		t.Fatalf("tool_loops = %v", snap["tool_loops"])
	}
}

type stubStatusPollExecutor struct{}

func (stubStatusPollExecutor) Definitions() []tools.ToolDef { return nil }

func (stubStatusPollExecutor) Execute(_ context.Context, name, _ string) (string, error) {
	if name == "read_file" {
		return "ok", nil
	}
	return "", nil
}

func (stubStatusPollExecutor) TakeBashCompressStatsForCall(string) map[string]any { return nil }
func (stubStatusPollExecutor) TakeToolResultMediaForCall(string) map[string]any   { return nil }
func (stubStatusPollExecutor) TakeReadImageVisionForCall(string) *tools.ReadImageVisionPayload {
	return nil
}
