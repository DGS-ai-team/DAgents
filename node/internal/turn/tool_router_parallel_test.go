package turn

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

// parallelDelayExecutor 按工具名阻塞不同时长，用于验证并行批次完成即推送 SSE。
type parallelDelayExecutor struct {
	delays  map[string]time.Duration
	started atomic.Int32
}

type cancelParallelExecutor struct {
	cancel  context.CancelFunc
	started atomic.Int32
}

func (s *cancelParallelExecutor) Definitions() []tools.ToolDef { return nil }

func (s *cancelParallelExecutor) Execute(_ context.Context, name, _ string) (string, error) {
	if s.started.Add(1) >= 2 {
		s.cancel()
	}
	return "ok:" + name, nil
}

func (s *cancelParallelExecutor) StartBackground(context.Context, string, string, string, string) (string, error) {
	return "", nil
}

func (s *cancelParallelExecutor) TakeBashCompressStatsForCall(string) map[string]any { return nil }
func (s *cancelParallelExecutor) TakeToolResultMediaForCall(string) map[string]any   { return nil }
func (s *cancelParallelExecutor) TakeReadImageVisionForCall(string) *tools.ReadImageVisionPayload {
	return nil
}

func (s *parallelDelayExecutor) Definitions() []tools.ToolDef { return nil }

func (s *parallelDelayExecutor) Execute(_ context.Context, name, _ string) (string, error) {
	s.started.Add(1)
	if d, ok := s.delays[name]; ok && d > 0 {
		time.Sleep(d)
	}
	return "ok:" + name, nil
}

func (s *parallelDelayExecutor) StartBackground(context.Context, string, string, string, string) (string, error) {
	return "", nil
}

func (s *parallelDelayExecutor) TakeBashCompressStatsForCall(string) map[string]any { return nil }
func (s *parallelDelayExecutor) TakeToolResultMediaForCall(string) map[string]any   { return nil }
func (s *parallelDelayExecutor) TakeReadImageVisionForCall(string) *tools.ReadImageVisionPayload {
	return nil
}

func TestExecuteAutoBatch_publishesResultAsEachToolCompletes(t *testing.T) {
	root := t.TempDir()
	hub := stream.NewHub(32, logx.Discard())
	exec := &parallelDelayExecutor{
		delays: map[string]time.Duration{
			"read_file":  20 * time.Millisecond,
			"list_dir":   200 * time.Millisecond,
			"grep_files": 200 * time.Millisecond,
		},
	}
	orch := NewOrchestrator(
		"a1", root, hub, &llm.MockClient{},
		exec,
		nil, SkillAccess{}, DefaultMaxToolLoops(), nil, nil,
		hooks.RuntimeConfig{},
		logx.Discard(),
	)

	calls := []llm.ToolCall{
		{ID: "c-fast", Type: "function", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"a"}`}},
		{ID: "c-slow1", Type: "function", Function: llm.ToolCallFunction{Name: "list_dir", Arguments: `{"path":"."}`}},
		{ID: "c-slow2", Type: "function", Function: llm.ToolCallFunction{Name: "grep_files", Arguments: `{"pattern":"x"}`}},
	}
	history := []llm.Message{{Role: "assistant", ToolCalls: calls}}

	ch := hub.Subscribe(hub.CurrentSeq())
	defer hub.Unsubscribe(ch)

	var firstResultID string
	var gotMu sync.Mutex
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range ch {
			if ev.Type != "tool_result" {
				continue
			}
			id, _ := ev.Data["tool_call_id"].(string)
			gotMu.Lock()
			if firstResultID == "" {
				firstResultID = id
			}
			gotMu.Unlock()
			if id == "c-fast" {
				// 快工具结果应在慢工具仍执行时到达（并行 + 完成即推送）
				if n := exec.started.Load(); n < 2 {
					t.Errorf("expected other tools to have started before fast result, started=%d", n)
				}
				return
			}
		}
	}()

	if err := orch.executeAutoBatch(context.Background(), "sess-parallel", &history, calls, nil); err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for fast tool_result")
	}

	gotMu.Lock()
	got := firstResultID
	gotMu.Unlock()
	if got != "c-fast" {
		t.Fatalf("first tool_result id=%q want c-fast (completion order)", got)
	}

	// history 仍按原始 tool_calls 顺序落盘
	if len(history) != 4 {
		t.Fatalf("history len=%d want 4", len(history))
	}
	for i, wantID := range []string{"c-fast", "c-slow1", "c-slow2"} {
		msg := history[i+1]
		if msg.Role != "tool" || msg.ToolCallID != wantID {
			t.Fatalf("history[%d]=role=%s id=%s want tool/%s", i+1, msg.Role, msg.ToolCallID, wantID)
		}
	}
}

func TestExecuteAutoBatch_persistsCompletedResultsBeforeCancellation(t *testing.T) {
	root := t.TempDir()
	exec := &cancelParallelExecutor{}
	orch := NewOrchestrator(
		"a1", root, stream.NewHub(32, logx.Discard()), &llm.MockClient{},
		exec,
		nil, SkillAccess{}, DefaultMaxToolLoops(), nil, nil,
		hooks.RuntimeConfig{},
		logx.Discard(),
	)

	ctx, cancel := context.WithCancel(context.Background())
	exec.cancel = cancel
	calls := []llm.ToolCall{
		{ID: "c-cancel-1", Type: "function", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"a"}`}},
		{ID: "c-cancel-2", Type: "function", Function: llm.ToolCallFunction{Name: "list_dir", Arguments: `{"path":"."}`}},
	}
	history := []llm.Message{{Role: "assistant", ToolCalls: calls}}

	err := orch.executeAutoBatch(ctx, "sess-cancel", &history, calls, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("executeAutoBatch err=%v, want context.Canceled", err)
	}
	if len(history) != 3 {
		t.Fatalf("history len=%d want 3 (assistant + 2 tool results)", len(history))
	}
	for i, wantID := range []string{"c-cancel-1", "c-cancel-2"} {
		msg := history[i+1]
		if msg.Role != "tool" || msg.ToolCallID != wantID {
			t.Fatalf("history[%d]=role=%s id=%s want tool/%s", i+1, msg.Role, msg.ToolCallID, wantID)
		}
	}
}
