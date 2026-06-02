package repl

import (
	"context"
	"strings"
	"sync"
	"testing"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
	clihitl "github.com/DGS-ai-team/DAgents/client/internal/hitl"
	tuishared "github.com/DGS-ai-team/DAgents/client/internal/tui/shared"
)

func newTestStreamRunner() *streamRunner {
	show := false
	return newStreamRunner(
		nil,
		"sess-1",
		tuishared.NewTranscript(0),
		&tuishared.ToolFold{},
		&sync.Mutex{},
		&show,
		nil,
	)
}

// TestHandleEventFiltersChildAssistant REPL 应隐藏子 turn assistant，与 full TUI 一致。
func TestHandleEventFiltersChildAssistant(t *testing.T) {
	r := newTestStreamRunner()
	ctx := context.Background()

	_, err := r.handleEvent(ctx, nodeapi.StreamEvent{
		Type: "assistant",
		Data: map[string]any{
			"child_session_id": "child-1",
			"content":          "secret",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.transcript.Len() != 0 {
		t.Fatalf("child assistant leaked, lines=%d", r.transcript.Len())
	}

	_, err = r.handleEvent(ctx, nodeapi.StreamEvent{
		Type: "assistant",
		Data: map[string]any{"content": "parent hi"},
	})
	if err != nil {
		t.Fatal(err)
	}
	r.finishAssistantLine()
	if len(r.transcript.Lines()) != 1 || !strings.Contains(r.transcript.Lines()[0], "parent hi") {
		t.Fatalf("parent assistant missing: %v", r.transcript.Lines())
	}
}

// TestHandleEventChildLifecycle REPL 应展示子 Agent 生命周期系统行。
func TestHandleEventChildLifecycle(t *testing.T) {
	r := newTestStreamRunner()
	ctx := context.Background()

	_, err := r.handleEvent(ctx, nodeapi.StreamEvent{
		Type: "child_agent_created",
		Data: map[string]any{
			"child_session_id": "child-abc",
			"purpose":          "scan logs",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	lines := r.transcript.Lines()
	if len(lines) != 1 || !strings.Contains(lines[0], "子任务已创建") {
		t.Fatalf("lifecycle line missing: %v", lines)
	}
}

// TestHandleEventSkipsChildToolResult 子 tool_result 不应进入 transcript。
func TestHandleEventSkipsChildToolResult(t *testing.T) {
	r := newTestStreamRunner()
	ctx := context.Background()

	_, err := r.handleEvent(ctx, nodeapi.StreamEvent{
		Type: "tool_result",
		Data: map[string]any{
			"child_session_id": "child-1",
			"tool_name":        "bash_run",
			"content":          "hidden",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.transcript.Len() != 0 {
		t.Fatalf("child tool_result leaked: %v", r.transcript.Lines())
	}
}

// TestShouldSkipChildRuntimeDisplayRepl 与 hitl scope 包约定一致（文档化 REPL 过滤边界）。
func TestShouldSkipChildRuntimeDisplayRepl(t *testing.T) {
	data := map[string]any{"child_session_id": "c1", "content": "x"}
	if !clihitl.ShouldSkipChildRuntimeDisplay("assistant", data) {
		t.Fatal("assistant should skip")
	}
	if clihitl.ShouldSkipChildRuntimeDisplay("approval_required", data) {
		t.Fatal("approval should not skip")
	}
	if clihitl.ShouldSkipChildRuntimeDisplay("child_agent_created", data) {
		t.Fatal("lifecycle should not skip")
	}
}
