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

func TestTurnContextMetrics_statusPollAndDoneSSE(t *testing.T) {
	hub := stream.NewHub(16, logx.Discard())
	orch := NewOrchestrator(
		"a1", t.TempDir(), hub, &llm.MockClient{},
		stubStatusPollExecutor{},
		nil, SkillAccess{}, DefaultMaxToolLoops(), nil, nil,
		hooks.RuntimeConfig{
			Duplicate:  hooks.DefaultDuplicateConfig(),
			ToolResult: hooks.DefaultToolResultConfig(t.TempDir()),
		},
		logx.Discard(),
	)
	orch.resetContextMetrics("sess-m")
	tcStatus := llm.ToolCall{
		ID: "call-status", Type: "function",
		Function: llm.ToolCallFunction{
			Name:      "background_job_status",
			Arguments: `{"call_purpose":"poll","job_id":"job-1"}`,
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
	if m == nil || m.StatusPollCount != 1 || m.ToolCalls != 1 {
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
	if n, ok := raw["status_poll_count"].(int); !ok || n != 1 {
		if f, ok := raw["status_poll_count"].(float64); !ok || int(f) != 1 {
			t.Fatalf("status_poll_count = %v (%T)", raw["status_poll_count"], raw["status_poll_count"])
		}
	}
}

func TestTurnContextMetrics_readFileEncodingAndRepeat(t *testing.T) {
	orch := NewOrchestrator("a1", t.TempDir(), stream.NewHub(4, logx.Discard()), &llm.MockClient{}, nil, nil, SkillAccess{}, 10, nil, nil, hooks.RuntimeConfig{Duplicate: hooks.DefaultDuplicateConfig()}, logx.Discard())
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
	orch := NewOrchestrator("a1", t.TempDir(), stream.NewHub(4, logx.Discard()), &llm.MockClient{}, nil, nil, SkillAccess{}, 10, nil, nil, hooks.RuntimeConfig{Duplicate: hooks.DefaultDuplicateConfig()}, logx.Discard())
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
	if name == "background_job_status" {
		return "status=running", nil
	}
	return "", nil
}

func (stubStatusPollExecutor) StartBackground(context.Context, string, string, string, string) (string, error) {
	return "", nil
}

func (stubStatusPollExecutor) TakeBashCompressStatsForCall(string) map[string]any { return nil }
func (stubStatusPollExecutor) TakeToolResultMediaForCall(string) map[string]any   { return nil }
func (stubStatusPollExecutor) TakeReadImageVisionForCall(string) *tools.ReadImageVisionPayload {
	return nil
}
